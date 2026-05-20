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
