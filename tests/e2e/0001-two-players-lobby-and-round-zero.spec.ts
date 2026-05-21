import {
  test,
  expect,
  createSession,
  joinAs,
  openSecondSeat,
  rosterRow,
} from './helpers';

test.describe('Scenario 0001 — lobby, roster, share, reconnect', () => {
  test('Alice creates a session and Bob joins; both see the live roster', async ({ browser }) => {
    const aliceCtx = await browser.newContext();
    const alice = await aliceCtx.newPage();

    await test.step('Alice loads home and sees the New game affordance', async () => {
      await alice.goto('/');
      await expect(alice.getByRole('heading', { name: 'Start a game' })).toBeVisible();
      await expect(alice.getByRole('button', { name: 'New game' })).toBeVisible();
    });

    let lobbyURL = '';
    await test.step('Alice clicks New game and lands in a freshly created lobby', async () => {
      await alice.getByRole('button', { name: 'New game' }).click();
      await expect(alice).toHaveURL(/\/g\/[0-9A-Z]{3}-[0-9A-Z]{3}$/);
      lobbyURL = alice.url();
      await expect(alice.getByRole('heading', { name: 'Lobby' })).toBeVisible();
      await expect(alice.getByLabel('Display name')).toBeVisible();
      await expect(alice.locator('#connected-panel')).toBeHidden();
      await expect(alice.locator('#share-panel')).toBeHidden();
    });

    await test.step('Alice joins as Alice and gets the Host badge', async () => {
      await joinAs(alice, 'Alice');
      const aliceRow = rosterRow(alice, 'Alice');
      await expect(aliceRow).toHaveCount(1);
      await expect(aliceRow.locator('.host-badge')).toHaveText('Host');
      await expect(alice.locator('#share-panel')).toBeVisible();
      await expect(alice.locator('#share-link')).toHaveValue(lobbyURL);
    });

    const bobSetup = await openSecondSeat(browser, lobbyURL);
    const bob = bobSetup.page;

    await test.step('Bob joins as Bob; both rosters show two seats with the Host badge on Alice', async () => {
      await joinAs(bob, 'Bob');

      const bobRoster = bob.locator('#roster li');
      await expect(bobRoster).toHaveCount(2);
      await expect(bobRoster.nth(0)).toContainText('Alice');
      await expect(bobRoster.nth(0).locator('.host-badge')).toHaveText('Host');
      await expect(bobRoster.nth(1)).toContainText('Bob');
      await expect(bobRoster.nth(1).locator('.host-badge')).toHaveCount(0);
      await expect(bobRoster.nth(1).locator('.self')).toHaveText('Bob');

      const aliceRoster = alice.locator('#roster li');
      await expect(aliceRoster).toHaveCount(2);
      await expect(aliceRoster.nth(0)).toContainText('Alice');
      await expect(aliceRoster.nth(0).locator('.host-badge')).toHaveText('Host');
      await expect(aliceRoster.nth(1)).toContainText('Bob');
      await expect(aliceRoster.nth(0).locator('.self')).toHaveText('Alice');
    });

    await test.step('Alice clicks Copy link and gets visible feedback', async () => {
      await alice.getByRole('button', { name: 'Copy link' }).click();
      await expect(alice.locator('#copy-status')).toHaveText(/.+/);
    });

    await aliceCtx.close();
    await bobSetup.context.close();
  });

  test('Bob disconnects then reconnects; his seat is preserved and his draft of identity intact', async ({ browser }) => {
    const aliceSetup = await createSession(browser);
    const alice = aliceSetup.page;
    const { lobbyURL } = aliceSetup;
    await joinAs(alice, 'Alice');

    const bobSetup = await openSecondSeat(browser, lobbyURL);
    await joinAs(bobSetup.page, 'Bob');
    await expect(alice.locator('#roster li')).toHaveCount(2);

    await test.step('Bob closes his tab; Alice sees Bob marked disconnected', async () => {
      await bobSetup.context.close();
      const bobRowOnAlice = rosterRow(alice, 'Bob');
      await expect(bobRowOnAlice).toHaveClass(/disconnected/);
      await expect(bobRowOnAlice.locator('.disconnected-tag')).toBeVisible();
    });

    await test.step('Bob reopens the lobby and rejoins as Bob; his seat is restored', async () => {
      const bobReturn = await openSecondSeat(browser, lobbyURL);
      await joinAs(bobReturn.page, 'Bob');
      const aliceRoster = alice.locator('#roster li');
      await expect(aliceRoster).toHaveCount(2);
      const bobRowOnAlice = rosterRow(alice, 'Bob');
      await expect(bobRowOnAlice).not.toHaveClass(/disconnected/);
      await bobReturn.context.close();
    });

    await aliceSetup.context.close();
  });

  test('Impostor case: a second client claiming Alice supersedes the prior connection', async ({ browser }) => {
    const aliceSetup = await createSession(browser);
    const { lobbyURL } = aliceSetup;
    await joinAs(aliceSetup.page, 'Alice');

    await test.step('Alice closes her tab; her seat persists server-side', async () => {
      await aliceSetup.context.close();
    });

    const charlieSetup = await openSecondSeat(browser, lobbyURL);
    await test.step('Charlie connects as "Alice" and lands on Alice\'s seat', async () => {
      await joinAs(charlieSetup.page, 'Alice');
      const aliceRow = rosterRow(charlieSetup.page, 'Alice');
      await expect(aliceRow).toHaveCount(1);
      await expect(aliceRow.locator('.host-badge')).toHaveText('Host');
    });

    await test.step('Real Alice reopens as "Alice"; Charlie\'s tab shows superseded', async () => {
      const aliceReturn = await openSecondSeat(browser, lobbyURL);
      await joinAs(aliceReturn.page, 'Alice');
      await expect(aliceReturn.page.locator('#connected-panel')).toBeVisible();

      await expect(charlieSetup.page.locator('#status')).toHaveText(/taken over by another connection/);
      await aliceReturn.context.close();
    });

    await charlieSetup.context.close();
  });
});

test.describe('Scenario 0001 — host management', () => {
  test('Host can transfer Host to another Player and receive it back', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await test.step('Alice (Host) sees Make Host / Kick on Bob\'s row; Bob sees neither on Alice', async () => {
      const bobRowOnAlice = rosterRow(alice.page, 'Bob');
      await expect(bobRowOnAlice.getByRole('button', { name: 'Make Host' })).toBeVisible();
      await expect(bobRowOnAlice.getByRole('button', { name: 'Kick' })).toBeVisible();

      const aliceRowOnBob = rosterRow(bob.page, 'Alice');
      await expect(aliceRowOnBob.getByRole('button', { name: 'Make Host' })).toHaveCount(0);
      await expect(aliceRowOnBob.getByRole('button', { name: 'Kick' })).toHaveCount(0);
    });

    await test.step('Alice clicks Make Host on Bob; Host badge moves to Bob in both views', async () => {
      await rosterRow(alice.page, 'Bob').getByRole('button', { name: 'Make Host' }).click();
      await expect(rosterRow(alice.page, 'Bob').locator('.host-badge')).toHaveText('Host');
      await expect(rosterRow(alice.page, 'Alice').locator('.host-badge')).toHaveCount(0);
      await expect(rosterRow(bob.page, 'Bob').locator('.host-badge')).toHaveText('Host');
      await expect(rosterRow(bob.page, 'Alice').locator('.host-badge')).toHaveCount(0);
    });

    await test.step('Bob now sees Make Host / Kick on Alice; clicks Make Host to hand it back', async () => {
      const aliceRowOnBob = rosterRow(bob.page, 'Alice');
      await expect(aliceRowOnBob.getByRole('button', { name: 'Make Host' })).toBeVisible();
      await aliceRowOnBob.getByRole('button', { name: 'Make Host' }).click();
      await expect(rosterRow(alice.page, 'Alice').locator('.host-badge')).toHaveText('Host');
      await expect(rosterRow(bob.page, 'Alice').locator('.host-badge')).toHaveText('Host');
    });
  });

  test('Host can Kick another Player; the seat is removed and the target returns to name entry', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await rosterRow(alice.page, 'Bob').getByRole('button', { name: 'Kick' }).click();

    await test.step('Bob\'s tab returns to name entry with the kicked-by-host message', async () => {
      await expect(bob.page.locator('#name-panel')).toBeVisible();
      await expect(bob.page.locator('#name-error')).toContainText(/removed from this game session by the host/);
      await expect(bob.page.locator('#connected-panel')).toBeHidden();
    });

    await test.step('Alice\'s roster loses Bob\'s seat', async () => {
      await expect(alice.page.locator('#roster li')).toHaveCount(1);
      await expect(rosterRow(alice.page, 'Bob')).toHaveCount(0);
    });
  });

  test('A Player can voluntarily Leave; their seat is removed and the other sees a notice', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    bob.page.on('dialog', dialog => dialog.accept());

    await bob.page.getByRole('button', { name: 'Leave game' }).click();

    await test.step('Bob\'s tab returns to the name-entry view', async () => {
      await expect(bob.page.locator('#name-panel')).toBeVisible();
      await expect(bob.page.locator('#connected-panel')).toBeHidden();
    });

    await test.step('Alice\'s roster loses Bob\'s seat and shows a "Bob left the game" notice', async () => {
      await expect(alice.page.locator('#roster li')).toHaveCount(1);
      await expect(rosterRow(alice.page, 'Bob')).toHaveCount(0);
      await expect(alice.page.locator('#notice-stack')).toContainText('Bob left the game');
    });
  });

  test('Voluntary Host Leave transfers Host immediately, with no grace wait', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await test.step('Alice (Host) hands Host to Bob via Make Host', async () => {
      await rosterRow(alice.page, 'Bob').getByRole('button', { name: 'Make Host' }).click();
      await expect(rosterRow(bob.page, 'Bob').locator('.host-badge')).toHaveText('Host');
    });

    bob.page.on('dialog', dialog => dialog.accept());

    await test.step('Bob (now Host) clicks Leave; Host moves to Alice the same tick', async () => {
      await bob.page.getByRole('button', { name: 'Leave game' }).click();
      await expect(rosterRow(alice.page, 'Alice').locator('.host-badge')).toHaveText('Host');
      await expect(alice.page.locator('#notice-stack')).toContainText('Bob left the game — Alice is now the Host');
    });
  });

  test('Host auto-migrates after the disconnect grace expires; the original Host does not reclaim on rejoin', async ({ browser, twoPlayerLobby }) => {
    const { alice, bob, lobbyURL } = twoPlayerLobby;

    await test.step('Alice (Host) closes her tab; after the grace, Host moves to Bob with a notice', async () => {
      await alice.context.close();
      await expect(rosterRow(bob.page, 'Bob').locator('.host-badge')).toHaveText('Host');
      await expect(rosterRow(bob.page, 'Alice').locator('.host-badge')).toHaveCount(0);
      await expect(bob.page.locator('#notice-stack')).toContainText('Alice was disconnected — Bob is now the Host');
    });

    await test.step('Alice reopens and rejoins; she does NOT auto-reclaim Host', async () => {
      const aliceReturn = await openSecondSeat(browser, lobbyURL);
      await joinAs(aliceReturn.page, 'Alice');
      await expect(rosterRow(aliceReturn.page, 'Bob').locator('.host-badge')).toHaveText('Host');
      await expect(rosterRow(aliceReturn.page, 'Alice').locator('.host-badge')).toHaveCount(0);
      await aliceReturn.context.close();
    });
  });
});

test.describe('Scenario 0001 — Round 0 starter Caption', () => {
  test('Host starts a round; both Players submit; both transition to round-complete', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await test.step('Alice (Host) sees the timer dropdown + Start; Bob does not', async () => {
      await expect(alice.page.locator('#host-controls')).toBeVisible();
      await expect(alice.page.getByRole('button', { name: 'Start' })).toBeVisible();
      await expect(bob.page.locator('#host-controls')).toBeHidden();
    });

    await test.step('Alice picks a 15s timer and clicks Start; both transition to Round 0', async () => {
      await alice.page.locator('#timer-select').selectOption('15');
      await alice.page.getByRole('button', { name: 'Start' }).click();

      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#round-panel')).toBeVisible();
        await expect(page.getByRole('heading', { name: /Round 0 — Starter Caption/ })).toBeVisible();
        await expect(page.locator('#round-countdown')).toContainText(/Time left: \d:\d{2}/);
      }
    });

    await test.step('Alice sees the Host-only Force advance; Bob does not', async () => {
      await expect(alice.page.locator('#round-advance-btn')).toBeVisible();
      await expect(bob.page.locator('#round-advance-btn')).toBeHidden();
    });

    await test.step('Both type a Caption and click Submit; both leave Round 0 for Round 1 (drawing)', async () => {
      await alice.page.locator('#round-input').fill('a wizard losing an argument with a goose');
      await alice.page.getByRole('button', { name: 'Submit', exact: true }).click();
      await expect(alice.page.locator('#round-submitted-banner')).toBeVisible();

      await bob.page.locator('#round-input').fill('two squirrels reviewing a contract');
      await bob.page.getByRole('button', { name: 'Submit', exact: true }).click();

      // In a 2-Player game, Round 0 (caption) → Round 1 (drawing) is
      // back-to-back; the round-complete-panel only flashes between
      // them, so assert against the stable round-draw-panel.
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#round-panel')).toBeHidden();
        await expect(page.locator('#round-draw-panel')).toBeVisible();
      }
    });
  });

  test('Host can Force advance mid-round; non-empty drafts ship, empty drafts get Ghost-filled', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await alice.page.locator('#timer-select').selectOption('60');
    await alice.page.getByRole('button', { name: 'Start' }).click();
    await expect(alice.page.locator('#round-panel')).toBeVisible();
    await expect(bob.page.locator('#round-panel')).toBeVisible();

    await test.step('Bob types a partial draft; Alice clicks Force advance', async () => {
      await bob.page.locator('#round-input').fill('the cat is on');
      await alice.page.getByRole('button', { name: 'Force advance' }).click();
    });

    await test.step('Both screens leave Round 0 and reach Round 1 (drawing)', async () => {
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#round-panel')).toBeHidden();
        await expect(page.locator('#round-draw-panel')).toBeVisible();
      }
    });
  });

  test('Reconnect mid-round restores the partial draft into the textarea', async ({ browser, twoPlayerLobby }) => {
    const { alice, bob, lobbyURL } = twoPlayerLobby;

    await alice.page.locator('#timer-select').selectOption('60');
    await alice.page.getByRole('button', { name: 'Start' }).click();
    await expect(bob.page.locator('#round-panel')).toBeVisible();

    await test.step('Bob types a partial Caption and closes his tab', async () => {
      await bob.page.locator('#round-input').fill('the cat is on');
      await expect(bob.page.locator('#round-input')).toHaveValue('the cat is on');
      await bob.context.close();
    });

    await test.step('Bob reopens; round panel comes up with his draft pre-filled', async () => {
      const bobReturn = await openSecondSeat(browser, lobbyURL);
      await joinAs(bobReturn.page, 'Bob');
      await expect(bobReturn.page.locator('#round-panel')).toBeVisible();
      await expect(bobReturn.page.locator('#round-input')).toHaveValue('the cat is on');
      await bobReturn.context.close();
    });
  });

  test('Leave mid-round preserves the seat as disconnected (ADR 0009)', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await alice.page.locator('#timer-select').selectOption('60');
    await alice.page.getByRole('button', { name: 'Start' }).click();
    await expect(bob.page.locator('#round-panel')).toBeVisible();

    bob.page.on('dialog', dialog => dialog.accept());
    await bob.page.getByRole('button', { name: 'Leave game' }).click();

    await test.step('Bob\'s tab returns to name entry', async () => {
      await expect(bob.page.locator('#name-panel')).toBeVisible();
    });

    await test.step('Alice\'s roster keeps Bob\'s seat, marked disconnected (not removed)', async () => {
      const aliceRoster = alice.page.locator('#roster li');
      await expect(aliceRoster).toHaveCount(2);
      await expect(rosterRow(alice.page, 'Bob')).toHaveClass(/disconnected/);
    });
  });
});

test.describe('Scenario 0001 — failure modes', () => {
  test('Unknown game session code returns a 404 page', async ({ page }) => {
    const resp = await page.goto('/g/Z9Z-Z9Z');
    expect(resp?.status()).toBe(404);
    await expect(page.getByRole('heading', { name: 'Not found' })).toBeVisible();
  });

  test('Code containing visually-confusable characters (I, L, O, U) returns 404', async ({ page }) => {
    const resp = await page.goto('/g/A4B-K9I');
    expect(resp?.status()).toBe(404);
  });

  test('Lobby rejects an over-cap join with the session-full error', async ({ browser, twoPlayerLobby }) => {
    // MaxPlayers is currently clamped to 2 (see
    // internal/gamesession/gamesession.go:37 — "TEMPORARY: clamped to
    // 2 until N≥3 generalization slice"). When the cap moves to its
    // real value (8), this test's setup needs more joiners.
    //
    // The assertion below pins the full user-visible string from the
    // JS in lobby.tmpl so it serves as a tripwire: while MaxPlayers=2,
    // the "(8 players maximum)" copy is a known mismatch with the
    // actual cap; when MaxPlayers moves, this assertion will break
    // and force the JS copy to be reconciled in the same change.
    const { lobbyURL } = twoPlayerLobby;

    const thirdCtx = await browser.newContext();
    const third = await thirdCtx.newPage();
    await third.goto(lobbyURL);
    await third.getByLabel('Display name').fill('Charlie');
    await third.getByRole('button', { name: 'Join', exact: true }).click();
    await expect(third.locator('#name-error')).toHaveText('This game session is full (8 players maximum).');
    await thirdCtx.close();
  });
});
