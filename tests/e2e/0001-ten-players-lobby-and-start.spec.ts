// Scope: lobby user journey at N=10 (Join × 10, capacity rejection,
// Host clicks Start, all clients transition). See ADR 0013 for the
// e2e admission rules.

import { test, expect, joinAs, joinN, openSecondSeat, rosterRow } from './helpers';

test.describe('Scenario 0001 — lobby journey at N=10', () => {
  test('Host joins, 9 more Players join, 11th is rejected, Host Starts; all 10 transition', async ({ browser }) => {
    // Alice creates a session and takes the first seat.
    const aliceCtx = await browser.newContext();
    const alice = await aliceCtx.newPage();
    await alice.goto('/');
    await alice.getByRole('button', { name: 'New game' }).click();
    await expect(alice).toHaveURL(/\/g\/[0-9A-Z]{3}-[0-9A-Z]{3}$/);
    const lobbyURL = alice.url();
    await joinAs(alice, 'Alice');
    await expect(rosterRow(alice, 'Alice').locator('.host-badge')).toHaveText('Host');

    // Bring the roster to 10 by adding 9 more seats.
    const others = ['Bob', 'Carol', 'Dave', 'Eve', 'Frank', 'Grace', 'Heidi', 'Ivan', 'Judy'];
    const seats = await joinN(browser, lobbyURL, others);

    try {
      await test.step('Alice and the last joiner both see a 10-seat roster with Alice still Host', async () => {
        await expect(alice.locator('#roster li')).toHaveCount(10);
        const lastSeat = seats[seats.length - 1].page;
        await expect(lastSeat.locator('#roster li')).toHaveCount(10);
        await expect(rosterRow(alice, 'Alice').locator('.host-badge')).toHaveText('Host');
      });

      await test.step('An 11th would-be Player is rejected with the session-full copy', async () => {
        const overflow = await openSecondSeat(browser, lobbyURL);
        try {
          await overflow.page.getByLabel('Display name').fill('Overflow');
          await overflow.page.getByRole('button', { name: 'Join', exact: true }).click();
          await expect(overflow.page.locator('#name-error')).toHaveText(
            'This game session is full (10 players maximum).',
          );
        } finally {
          await overflow.context.close();
        }
      });

      await test.step('Alice clicks Start; all 10 clients leave the lobby for Round 0', async () => {
        await alice.locator('#timer-select').selectOption('60');
        await alice.getByRole('button', { name: 'Start' }).click();
        await expect(alice.locator('#round-panel')).toBeVisible();
        for (const seat of seats) {
          await expect(seat.page.locator('#round-panel')).toBeVisible();
        }
      });
    } finally {
      for (const seat of seats) {
        await seat.context.close();
      }
      await aliceCtx.close();
    }
  });
});
