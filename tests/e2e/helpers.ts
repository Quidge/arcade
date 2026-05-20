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

export const test = base.extend<{ twoPlayerLobby: TwoPlayerLobby }>({
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

    await aliceSetup.context.close();
    await bobSetup.context.close();
  },
});

export { expect } from '@playwright/test';
