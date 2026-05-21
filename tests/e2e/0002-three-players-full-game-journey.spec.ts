// Scope: full-game user journey at N=3 (Start through every Round
// through per-Chain Reveal through End game). See ADR 0013 for the
// e2e admission rules.

import {
  test,
  expect,
  drawStroke,
  submitDrawing,
} from './helpers';
import type { Page } from '@playwright/test';

async function submitCaptionAt(page: Page, text: string) {
  await page.locator('#round-input').fill(text);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
}

test.describe('Scenario 0002 — full-game journey at N=3', () => {
  test('Three Players play through all Rounds, walk every Chain in Reveal, then Host ends the game', async ({ threePlayerLobby }) => {
    const { alice, bob, carol } = threePlayerLobby;
    const everyone: Page[] = [alice.page, bob.page, carol.page];

    await test.step('Alice (Host) sets a 60s timer and clicks Start; all three transition to Round 0', async () => {
      await alice.page.locator('#timer-select').selectOption('60');
      await alice.page.getByRole('button', { name: 'Start' }).click();
      for (const page of everyone) {
        await expect(page.locator('#round-panel')).toBeVisible();
        await expect(page.getByRole('heading', { name: /Round 0 — Starter Caption/ })).toBeVisible();
      }
    });

    await test.step('Round 0: each Player submits a starter Caption; all three move to Round 1 (drawing)', async () => {
      await submitCaptionAt(alice.page, 'a wizard losing an argument with a goose');
      await submitCaptionAt(bob.page, 'two squirrels reviewing a contract');
      await submitCaptionAt(carol.page, 'three penguins waiting in line for an espresso');
      for (const page of everyone) {
        await expect(page.locator('#round-draw-panel')).toBeVisible();
      }
    });

    await test.step('Round 1: each Player draws a stroke responding to the prior Caption and submits; all three move to Round 2 (guess caption)', async () => {
      await drawStroke(alice.page, [[0.2, 0.2], [0.5, 0.4], [0.8, 0.7]]);
      await submitDrawing(alice.page);
      await drawStroke(bob.page, [[0.3, 0.7], [0.5, 0.5], [0.7, 0.3]]);
      await submitDrawing(bob.page);
      await drawStroke(carol.page, [[0.2, 0.8], [0.4, 0.6], [0.6, 0.4]]);
      await submitDrawing(carol.page);
      for (const page of everyone) {
        await expect(page.locator('#round-panel')).toBeVisible();
      }
    });

    await test.step('Round 2: each Player submits a guess Caption; all three transition to Reveal', async () => {
      await submitCaptionAt(alice.page, 'the goose is winning');
      await submitCaptionAt(bob.page, 'two woodland creatures sign paperwork');
      await submitCaptionAt(carol.page, 'a tidy queue of small birds');
      for (const page of everyone) {
        await expect(page.locator('#reveal-panel')).toBeVisible();
      }
    });

    await test.step("Reveal opens on Alice's Chain with Alice driving and her starter Caption visible", async () => {
      for (const page of everyone) {
        await expect(page.locator('#reveal-header')).toHaveText("Alice's Chain");
      }
      await expect(alice.page.locator('#reveal-driver-line')).toHaveText("You're driving.");
      const aliceCards = alice.page.locator('.reveal-entry');
      await expect(aliceCards).toHaveCount(1);
      await expect(aliceCards.first().locator('.reveal-author')).toHaveText('Alice');
      await expect(aliceCards.first().locator('.reveal-caption')).toHaveText('a wizard losing an argument with a goose');
    });

    await test.step("Alice walks her Chain through every Entry (step, step, step, whole-chain) and on to Bob's Chain", async () => {
      await alice.page.getByRole('button', { name: 'Next' }).click(); // → drawing entry
      await expect(alice.page.locator('.reveal-entry')).toHaveCount(2);
      await alice.page.getByRole('button', { name: 'Next' }).click(); // → guess caption entry
      await expect(alice.page.locator('.reveal-entry')).toHaveCount(3);
      await alice.page.getByRole('button', { name: 'Next' }).click(); // → whole-chain view
      await expect(alice.page.locator('#reveal-meta')).toHaveText('Whole chain');
      await alice.page.getByRole('button', { name: 'Next' }).click(); // → Bob's Chain
      for (const page of everyone) {
        await expect(page.locator('#reveal-header')).toHaveText("Bob's Chain");
      }
      await expect(bob.page.locator('#reveal-driver-line')).toHaveText("You're driving.");
    });

    await test.step("Bob walks his Chain to whole-chain then on to Carol's Chain", async () => {
      await bob.page.getByRole('button', { name: 'Next' }).click();
      await bob.page.getByRole('button', { name: 'Next' }).click();
      await bob.page.getByRole('button', { name: 'Next' }).click();
      await bob.page.getByRole('button', { name: 'Next' }).click(); // → Carol's Chain
      for (const page of everyone) {
        await expect(page.locator('#reveal-header')).toHaveText("Carol's Chain");
      }
      await expect(carol.page.locator('#reveal-driver-line')).toHaveText("You're driving.");
    });

    await test.step('Carol walks her Chain to completion; all three see the reveal-complete driver line', async () => {
      await carol.page.getByRole('button', { name: 'Next' }).click();
      await carol.page.getByRole('button', { name: 'Next' }).click();
      await carol.page.getByRole('button', { name: 'Next' }).click();
      await carol.page.getByRole('button', { name: 'Next' }).click();
      for (const page of everyone) {
        await expect(page.locator('#reveal-driver-line')).toHaveText('Reveal complete — host can end the game.');
        await expect(page.getByRole('button', { name: 'Next' })).toBeHidden();
      }
    });

    await test.step('Alice (Host) clicks End game; all three browsers land on Thanks for playing', async () => {
      alice.page.on('dialog', d => d.accept());
      await alice.page.getByRole('button', { name: 'End game' }).click();
      for (const page of everyone) {
        await expect(page.locator('#ended-panel')).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Thanks for playing!' })).toBeVisible();
      }
    });
  });
});
