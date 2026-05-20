import { test, expect } from '@playwright/test';

test.describe('Scenario 0001 — two-player lobby (steps 1–7)', () => {
  test('Alice creates a session and Bob joins; both see the live roster', async ({ browser }) => {
    const aliceCtx = await browser.newContext();
    const bobCtx = await browser.newContext();
    const alice = await aliceCtx.newPage();
    const bob = await bobCtx.newPage();

    await alice.goto('/');
    await expect(alice.getByRole('heading', { name: 'Start a game' })).toBeVisible();
    await expect(alice.getByRole('button', { name: 'New game' })).toBeVisible();

    await alice.getByRole('button', { name: 'New game' }).click();
    await expect(alice).toHaveURL(/\/g\/[0-9A-Z]{3}-[0-9A-Z]{3}$/);
    const lobbyURL = alice.url();
    await expect(alice.getByRole('heading', { name: 'Lobby' })).toBeVisible();
    await expect(alice.locator('#name-input')).toBeVisible();
    await expect(alice.locator('#connected-panel')).toBeHidden();
    await expect(alice.locator('#share-panel')).toBeHidden();

    await alice.locator('#name-input').fill('Alice');
    await alice.locator('#name-submit').click();
    await expect(alice.locator('#name-panel')).toBeHidden();
    await expect(alice.locator('#connected-panel')).toBeVisible();
    const aliceRowOnAlice = alice.locator('#roster li').filter({ hasText: 'Alice' });
    await expect(aliceRowOnAlice).toHaveCount(1);
    await expect(aliceRowOnAlice.locator('.host-badge')).toHaveText('Host');
    await expect(alice.locator('#share-panel')).toBeVisible();
    await expect(alice.locator('#share-link')).toHaveValue(lobbyURL);

    await bob.goto(lobbyURL);
    await expect(bob).toHaveURL(lobbyURL);
    await expect(bob.locator('#name-input')).toBeVisible();
    await expect(bob.locator('#connected-panel')).toBeHidden();

    await bob.locator('#name-input').fill('Bob');
    await bob.locator('#name-submit').click();
    await expect(bob.locator('#name-panel')).toBeHidden();
    await expect(bob.locator('#connected-panel')).toBeVisible();
    const bobRosterItems = bob.locator('#roster li');
    await expect(bobRosterItems).toHaveCount(2);
    await expect(bobRosterItems.nth(0)).toContainText('Alice');
    await expect(bobRosterItems.nth(0).locator('.host-badge')).toHaveText('Host');
    await expect(bobRosterItems.nth(1)).toContainText('Bob');
    await expect(bobRosterItems.nth(1).locator('.host-badge')).toHaveCount(0);
    await expect(bobRosterItems.nth(1).locator('.self')).toHaveText('Bob');

    const aliceRosterItems = alice.locator('#roster li');
    await expect(aliceRosterItems).toHaveCount(2);
    await expect(aliceRosterItems.nth(0)).toContainText('Alice');
    await expect(aliceRosterItems.nth(0).locator('.host-badge')).toHaveText('Host');
    await expect(aliceRosterItems.nth(1)).toContainText('Bob');
    await expect(aliceRosterItems.nth(0).locator('.self')).toHaveText('Alice');

    await alice.locator('#copy-link').click();
    await expect(alice.locator('#copy-status')).toHaveText(/.+/);

    await aliceCtx.close();
    await bobCtx.close();
  });
});
