import {
  test,
  expect,
  joinAs,
  openSecondSeat,
  startRound,
  drawStroke,
  submitDrawing,
  reachRoundOne,
  completeRoundOne,
} from './helpers';

test.describe('Scenario 0002 — Round 1 drawing', () => {
  test('Both players draw, submit, and the room advances to Reveal', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;

    await reachRoundOne(alice.page, bob.page, '60');

    await test.step('Drawing prompts cross-pollinate: each player sees the other\'s Round 0 caption', async () => {
      await expect(alice.page.locator('#round-draw-prompt')).toHaveText('two squirrels reviewing a contract');
      await expect(bob.page.locator('#round-draw-prompt')).toHaveText('a wizard losing an argument with a goose');
    });

    await test.step('Both canvases and submit affordances are visible; Host sees Force advance, non-Host does not', async () => {
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#round-canvas')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Undo last stroke' })).toBeVisible();
        await expect(page.locator('#round-draw-submit-btn')).toBeVisible();
        await expect(page.locator('#round-draw-countdown')).toContainText(/Time left: \d:\d{2}/);
      }
      await expect(alice.page.locator('#round-draw-advance-btn')).toBeVisible();
      await expect(bob.page.locator('#round-draw-advance-btn')).toBeHidden();
    });

    await test.step('Alice draws strokes and submits; her canvas locks', async () => {
      await drawStroke(alice.page, [[0.2, 0.2], [0.4, 0.3], [0.6, 0.5]]);
      await submitDrawing(alice.page);
      await expect(alice.page.locator('#round-draw-submitted-banner')).toBeVisible();
      await expect(alice.page.locator('#round-draw-submit-btn')).toBeDisabled();
      await expect(alice.page.locator('#round-undo-btn')).toBeDisabled();
    });

    await test.step('Bob draws and submits; both screens transition to Reveal', async () => {
      await drawStroke(bob.page, [[0.3, 0.7], [0.5, 0.5], [0.7, 0.3]]);
      await submitDrawing(bob.page);
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#round-draw-panel')).toBeHidden();
        await expect(page.locator('#reveal-panel')).toBeVisible();
      }
    });

    // Stroke privacy mid-Round (the in-flight strokes are not echoed
    // to the other player's canvas) is a wire-level invariant asserted
    // by TestRoundOneInProgressStrokesArePrivate in tests/integration/.
  });

  test('Undo last stroke removes the most recent stroke', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;
    await reachRoundOne(alice.page, bob.page, '60');

    // We can't read the canvas pixel buffer to count strokes from a
    // headless browser without flake. Instead, exercise the affordance
    // end-to-end: after drawing then undoing, submission still works
    // and the round advances normally. The wire-level stroke count is
    // asserted in the integration tests.
    await drawStroke(alice.page, [[0.1, 0.1], [0.2, 0.2]]);
    await drawStroke(alice.page, [[0.3, 0.3], [0.4, 0.4]]);
    await drawStroke(alice.page, [[0.5, 0.5], [0.6, 0.6]]);
    await alice.page.getByRole('button', { name: 'Undo last stroke' }).click();

    await submitDrawing(alice.page);
    await drawStroke(bob.page, [[0.5, 0.5]]);
    await submitDrawing(bob.page);
    await expect(alice.page.locator('#reveal-panel')).toBeVisible();
  });
});

test.describe('Scenario 0002 — Reveal', () => {
  test('Reveal walks Alice\'s Chain then Bob\'s Chain to complete; End game tears the room down', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;
    await reachRoundOne(alice.page, bob.page, '60');
    await completeRoundOne(alice.page, bob.page);

    await test.step('Reveal opens on Alice\'s Chain with Alice driving', async () => {
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#reveal-header')).toHaveText("Alice's Chain");
      }
      await expect(alice.page.locator('#reveal-driver-line')).toHaveText("You're driving.");
      await expect(bob.page.locator('#reveal-driver-line')).toHaveText('Watching — Alice is driving.');
      await expect(alice.page.getByRole('button', { name: 'Next' })).toBeVisible();
      await expect(bob.page.getByRole('button', { name: 'Next' })).toBeHidden();

      const aliceCards = alice.page.locator('.reveal-entry');
      await expect(aliceCards).toHaveCount(1);
      await expect(aliceCards.first().locator('.reveal-author')).toHaveText('Alice');
      await expect(aliceCards.first().locator('.reveal-caption')).toHaveText('a wizard losing an argument with a goose');
    });

    await test.step('Alice clicks Next: drawing card appears (Bob\'s drawing on Alice\'s Chain)', async () => {
      await alice.page.getByRole('button', { name: 'Next' }).click();
      const aliceCards = alice.page.locator('.reveal-entry');
      await expect(aliceCards).toHaveCount(2);
      await expect(aliceCards.nth(1).locator('.reveal-author')).toHaveText('Bob');
      await expect(aliceCards.nth(1).locator('.reveal-drawing')).toBeVisible();
      await expect(alice.page.locator('#reveal-meta')).toHaveText('Step 2');
    });

    await test.step('Alice clicks Next: meta flips to "Whole chain"; header still Alice\'s Chain', async () => {
      await alice.page.getByRole('button', { name: 'Next' }).click();
      await expect(alice.page.locator('#reveal-meta')).toHaveText('Whole chain');
      await expect(alice.page.locator('#reveal-header')).toHaveText("Alice's Chain");
    });

    await test.step('Alice clicks Next: room flips to Bob\'s Chain with Bob driving', async () => {
      await alice.page.getByRole('button', { name: 'Next' }).click();
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#reveal-header')).toHaveText("Bob's Chain");
      }
      await expect(bob.page.locator('#reveal-driver-line')).toHaveText("You're driving.");
      await expect(alice.page.locator('#reveal-driver-line')).toHaveText('Watching — Bob is driving.');
      await expect(bob.page.getByRole('button', { name: 'Next' })).toBeVisible();
      await expect(alice.page.getByRole('button', { name: 'Next' })).toBeHidden();
    });

    await test.step('Bob walks his Chain through step → full → complete', async () => {
      await bob.page.getByRole('button', { name: 'Next' }).click(); // step 1 → step 2
      await expect(bob.page.locator('.reveal-entry')).toHaveCount(2);
      await bob.page.getByRole('button', { name: 'Next' }).click(); // → full
      await expect(bob.page.locator('#reveal-meta')).toHaveText('Whole chain');
      await bob.page.getByRole('button', { name: 'Next' }).click(); // → complete
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#reveal-driver-line')).toHaveText('Reveal complete — host can end the game.');
        await expect(page.getByRole('button', { name: 'Next' })).toBeHidden();
      }
    });

    await test.step('Alice (Host) clicks End game; both browsers land on Thanks for playing', async () => {
      alice.page.on('dialog', d => d.accept());
      await alice.page.getByRole('button', { name: 'End game' }).click();
      for (const page of [alice.page, bob.page]) {
        await expect(page.locator('#ended-panel')).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Thanks for playing!' })).toBeVisible();
      }
    });
  });

  test('Driver fallback: when starter is absent, Host drives; on rejoin, control snaps back', async ({ browser, twoPlayerLobby }) => {
    const { alice, bob, lobbyURL } = twoPlayerLobby;
    await reachRoundOne(alice.page, bob.page, '60');
    await completeRoundOne(alice.page, bob.page);

    await test.step('Alice walks her own Chain through step → step → full', async () => {
      await alice.page.getByRole('button', { name: 'Next' }).click(); // → step 2
      await alice.page.getByRole('button', { name: 'Next' }).click(); // → full
      await expect(alice.page.locator('#reveal-meta')).toHaveText('Whole chain');
    });

    await test.step('Bob closes his tab; Alice clicks Next and inherits driver of Bob\'s Chain by fallback', async () => {
      await bob.context.close();
      await alice.page.getByRole('button', { name: 'Next' }).click();
      await expect(alice.page.locator('#reveal-header')).toHaveText("Bob's Chain");
      await expect(alice.page.locator('#reveal-driver-line')).toHaveText("You're driving.");
      await expect(alice.page.getByRole('button', { name: 'Next' })).toBeVisible();
    });

    await test.step('Bob reopens and rejoins; his screen lands in the Reveal panel with him driving', async () => {
      const bobReturn = await openSecondSeat(browser, lobbyURL);
      await joinAs(bobReturn.page, 'Bob');
      await expect(bobReturn.page.locator('#reveal-panel')).toBeVisible();
      await expect(bobReturn.page.locator('#reveal-header')).toHaveText("Bob's Chain");
      await expect(bobReturn.page.locator('#reveal-driver-line')).toHaveText("You're driving.");

      // Bob's click triggers a fresh broadcast; Alice's screen now flips
      // to "Watching — Bob is driving." per the per-command re-eval rule.
      await bobReturn.page.getByRole('button', { name: 'Next' }).click();
      await expect(alice.page.locator('#reveal-driver-line')).toHaveText('Watching — Bob is driving.');
      await bobReturn.context.close();
    });
  });
});

test.describe('Scenario 0002 — End game', () => {
  test('Host can End game mid-Round 0; both screens land on Thanks for playing', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;
    await startRound(alice.page, '60');
    await expect(bob.page.locator('#round-panel')).toBeVisible();

    alice.page.on('dialog', d => d.accept());
    await alice.page.getByRole('button', { name: 'End game' }).click();

    for (const page of [alice.page, bob.page]) {
      await expect(page.locator('#ended-panel')).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Thanks for playing!' })).toBeVisible();
    }

    // End-game by a non-Host (Bob forging an end-game envelope via WS)
    // is silently rejected by the server. Not reachable from the UI —
    // covered by integration tests.
  });
});

test.describe('Scenario 0002 — Ghost responses', () => {
  test('Empty drawing slot gets Ghost-filled and shows the Ghost tag in Reveal', async ({ twoPlayerLobby }) => {
    const { alice, bob } = twoPlayerLobby;
    await reachRoundOne(alice.page, bob.page, '60');

    await test.step('Alice draws and submits; Bob leaves his canvas empty; Alice (Host) Force advances', async () => {
      await drawStroke(alice.page, [[0.2, 0.3], [0.6, 0.5], [0.8, 0.7]]);
      await submitDrawing(alice.page);
      await alice.page.getByRole('button', { name: 'Force advance' }).click();
      await expect(alice.page.locator('#reveal-panel')).toBeVisible();
      await expect(bob.page.locator('#reveal-panel')).toBeVisible();
    });

    await test.step('Walk Alice\'s Chain to step 2 (the drawing); Bob\'s slot is tagged as Ghost', async () => {
      await alice.page.getByRole('button', { name: 'Next' }).click();
      const drawingCard = alice.page.locator('.reveal-entry').nth(1);
      await expect(drawingCard.locator('.reveal-author')).toHaveText('Bob');
      await expect(drawingCard.locator('.reveal-ghost-tag')).toHaveText("Bob's Ghost");
      await expect(drawingCard.locator('.reveal-drawing')).toBeVisible();
    });
  });
});
