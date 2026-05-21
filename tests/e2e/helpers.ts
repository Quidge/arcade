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

export type TwoPlayerLobby = {
  alice: Seat;
  bob: Seat;
  lobbyURL: string;
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
  await page.getByRole('button', { name: 'New game' }).click();
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

export async function startRound(hostPage: Page, timerSeconds: string): Promise<void> {
  await hostPage.locator('#timer-select').selectOption(timerSeconds);
  await hostPage.getByRole('button', { name: 'Start' }).click();
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

// reachRoundOne walks both players from a fresh two-player lobby
// through Round 0 (each submits a starter caption) and into the
// Round 1 drawing panel. Used by any scenario that needs Round 1 or
// later as a starting state.
export async function reachRoundOne(
  alicePage: Page,
  bobPage: Page,
  timerSeconds: string,
): Promise<void> {
  await startRound(alicePage, timerSeconds);
  await expect(alicePage.locator('#round-panel')).toBeVisible();
  await expect(bobPage.locator('#round-panel')).toBeVisible();
  await submitCaption(alicePage, 'a wizard losing an argument with a goose');
  await submitCaption(bobPage, 'two squirrels reviewing a contract');
  for (const page of [alicePage, bobPage]) {
    await expect(page.locator('#round-draw-panel')).toBeVisible();
  }
}

// completeRoundOne draws a short stroke for each player and submits
// both, transitioning the room into the Reveal phase.
export async function completeRoundOne(alicePage: Page, bobPage: Page): Promise<void> {
  await drawStroke(alicePage, [[0.2, 0.2], [0.6, 0.4], [0.8, 0.7]]);
  await drawStroke(bobPage, [[0.3, 0.7], [0.5, 0.5], [0.7, 0.3]]);
  await submitDrawing(alicePage);
  await submitDrawing(bobPage);
  for (const page of [alicePage, bobPage]) {
    await expect(page.locator('#reveal-panel')).toBeVisible();
  }
}

export const test = base.extend<{
  twoPlayerLobby: TwoPlayerLobby;
  threePlayerLobby: ThreePlayerLobby;
}>({
  twoPlayerLobby: async ({ browser }, use) => {
    const aliceSetup = await createSession(browser);
    await joinAs(aliceSetup.page, 'Alice');

    const bobSetup = await openSecondSeat(browser, aliceSetup.lobbyURL);
    await joinAs(bobSetup.page, 'Bob');

    await expect(aliceSetup.page.locator('#roster li')).toHaveCount(2);
    await expect(bobSetup.page.locator('#roster li')).toHaveCount(2);

    await use({
      alice: { context: aliceSetup.context, page: aliceSetup.page, name: 'Alice' },
      bob: { context: bobSetup.context, page: bobSetup.page, name: 'Bob' },
      lobbyURL: aliceSetup.lobbyURL,
    });

    // Tests that close alice/bob contexts mid-flow (e.g. disconnect
    // simulations) leave the underlying contexts already closed when
    // teardown runs. Playwright's BrowserContext.close() is documented
    // as safe to call multiple times, so these calls are no-ops in
    // that case.
    await aliceSetup.context.close();
    await bobSetup.context.close();
  },
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
