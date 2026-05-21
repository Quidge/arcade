// Locator strategy: role-first (getByRole/getByLabel) for affordances
// users would describe by name; falls back to CSS IDs only where a
// role-based locator isn't stable (e.g., #round-canvas, the two
// distinct Submit buttons on the round and draw panels).

import { test as base, expect, type Browser, type BrowserContext, type Page } from '@playwright/test';

export type Seat = {
  context: BrowserContext;
  page: Page;
  name: string;
};

export type ThreePlayerLobby = {
  alice: Seat;
  bob: Seat;
  carol: Seat;
  lobbyURL: string;
};

export async function createSession(browser: Browser): Promise<{ context: BrowserContext; page: Page; lobbyURL: string }> {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('/');
  await page.getByRole('button', { name: 'Host a new game' }).click();
  // Matches the URL shape of a join-code path — a superset of joincode.Alphabet.
  await expect(page).toHaveURL(/\/g\/[0-9A-Z]{3}-[0-9A-Z]{3}$/);
  return { context, page, lobbyURL: page.url() };
}

export async function openSecondSeat(browser: Browser, lobbyURL: string): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto(lobbyURL);
  await expect(page.getByRole('heading', { name: 'Lobby' })).toBeVisible();
  return { context, page };
}

// joinN opens n browser contexts at lobbyURL and joins each one with
// the given names. Used by the N=10 lobby smoke test. Returns the
// per-seat handles in the same order as names so callers can refer
// to them positionally (seats[0] is the Host, by convention).
export async function joinN(
  browser: Browser,
  lobbyURL: string,
  names: string[],
): Promise<Array<{ context: BrowserContext; page: Page; name: string }>> {
  const seats: Array<{ context: BrowserContext; page: Page; name: string }> = [];
  for (const name of names) {
    const seat = await openSecondSeat(browser, lobbyURL);
    await joinAs(seat.page, name);
    seats.push({ context: seat.context, page: seat.page, name });
  }
  return seats;
}

export async function joinAs(page: Page, name: string): Promise<void> {
  await page.getByLabel('Display name').fill(name);
  await page.getByRole('button', { name: 'Join', exact: true }).click();
  await expect(page.locator('#connected-panel')).toBeVisible();
}

export function rosterRow(page: Page, name: string) {
  return page.locator('#roster li').filter({ hasText: name });
}

export async function submitCaption(page: Page, text: string): Promise<void> {
  await page.locator('#round-input').fill(text);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
}

// drawStroke synthesizes a pointer-down → moves → pointer-up sequence
// over #round-canvas using normalized [0..1] coords mapped against the
// canvas's current bounding box. The Round 1 drawing handler listens
// on Pointer Events, which Playwright's mouse APIs raise.
export async function drawStroke(page: Page, points: Array<[number, number]>): Promise<void> {
  if (points.length === 0) return;
  const canvas = page.locator('#round-canvas');
  const box = await canvas.boundingBox();
  if (!box) throw new Error('round canvas not in viewport');
  const at = (n: [number, number]): [number, number] => [box.x + n[0] * box.width, box.y + n[1] * box.height];
  const [sx, sy] = at(points[0]);
  await page.mouse.move(sx, sy);
  await page.mouse.down();
  for (let i = 1; i < points.length; i++) {
    const [px, py] = at(points[i]);
    await page.mouse.move(px, py, { steps: 5 });
  }
  await page.mouse.up();
}

export async function submitDrawing(page: Page): Promise<void> {
  await page.locator('#round-draw-submit-btn').click();
}

export const test = base.extend<{
  threePlayerLobby: ThreePlayerLobby;
}>({
  threePlayerLobby: async ({ browser }, use) => {
    const aliceSetup = await createSession(browser);
    await joinAs(aliceSetup.page, 'Alice');

    const bobSetup = await openSecondSeat(browser, aliceSetup.lobbyURL);
    await joinAs(bobSetup.page, 'Bob');

    const carolSetup = await openSecondSeat(browser, aliceSetup.lobbyURL);
    await joinAs(carolSetup.page, 'Carol');

    for (const page of [aliceSetup.page, bobSetup.page, carolSetup.page]) {
      await expect(page.locator('#roster li')).toHaveCount(3);
    }

    await use({
      alice: { context: aliceSetup.context, page: aliceSetup.page, name: 'Alice' },
      bob: { context: bobSetup.context, page: bobSetup.page, name: 'Bob' },
      carol: { context: carolSetup.context, page: carolSetup.page, name: 'Carol' },
      lobbyURL: aliceSetup.lobbyURL,
    });

    await aliceSetup.context.close();
    await bobSetup.context.close();
    await carolSetup.context.close();
  },
});

export { expect } from '@playwright/test';
