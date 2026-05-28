// Scope: lobby user journey at N=10 (Join × 10, capacity rejection,
// Host clicks Start, all clients transition). See ADR 0013 for the
// e2e admission rules.

import { test, expect, joinAs, joinN, openSecondSeat, rosterRow } from './helpers';

test.describe('Scenario 0001 — lobby journey at N=10', () => {
  test('Host joins, 9 more Players join, 11th is rejected, Host Starts; all 10 transition', async ({ browser }) => {
    // Alice creates a session and takes the first seat. She goes
    // through the Arcade picker (root → Scribble) before hosting,
    // exercising the real navigation path (ADR 0015).
    const aliceCtx = await browser.newContext();
    const alice = await aliceCtx.newPage();
    await alice.goto('/');
    await alice.getByRole('link', { name: 'Scribble' }).click();
    await expect(alice).toHaveURL(/\/scribble\/$/);
    await alice.getByRole('button', { name: 'Host a new game' }).click();
    // Matches the slugged join-code path — a superset of joincode.Alphabet.
    await expect(alice).toHaveURL(/\/scribble\/g\/[0-9A-Z]{3}-[0-9A-Z]{3}$/);
    const lobbyURL = alice.url();
    // Matches the slugged join-code path — a superset of joincode.Alphabet.
    const lobbyCode = lobbyURL.match(/\/g\/([0-9A-Z]{3}-[0-9A-Z]{3})$/)![1];
    await joinAs(alice, 'Alice');
    await expect(rosterRow(alice, 'Alice').locator('.host-badge')).toHaveText('Host');

    // Bob — the first joiner — arrives via the Scribble home's code
    // input (the join-by-code box stays on /scribble/, not the Arcade
    // root; see ADR 0015). This exercises the real joiner journey
    // (they have a code, not a URL). Subsequent joiners use the
    // direct-URL helper.
    const bobCtx = await browser.newContext();
    const bobPage = await bobCtx.newPage();
    await bobPage.goto('/scribble/');
    await bobPage.getByLabel('Game code').fill(lobbyCode);
    await bobPage.getByRole('button', { name: 'Join', exact: true }).click();
    await expect(bobPage).toHaveURL(lobbyURL);
    await joinAs(bobPage, 'Bob');
    const bobSeat = { context: bobCtx, page: bobPage, name: 'Bob' };

    // Bring the roster to 10 by adding 8 more seats via direct URL.
    const others = ['Carol', 'Dave', 'Eve', 'Frank', 'Grace', 'Heidi', 'Ivan', 'Judy'];
    const seats = [bobSeat, ...(await joinN(browser, lobbyURL, others))];

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
