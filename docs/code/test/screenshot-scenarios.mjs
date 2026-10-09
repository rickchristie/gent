// Every branch gets a desktop capture. Mobile repeats layouts that change materially, while the
// continuous page sweep covers all chapter content at both sizes. Avoid a Cartesian-product suite
// of identical labels: capture the actual differences readers must understand.
export async function captureStates(helpers) {
  const { page, profile, payload, capture, inspect, scrollTo, setRange } = helpers;
  const click = (selector) => page.locator(selector).click();
  const desktop = profile.name === 'desktop';
  if (profile.name === 'tablet') return;

  const steps = desktop ? payload.steps.map((_, index) => index) : [0, 5, 10];
  for (const index of steps) {
    await setRange('#run-slider', index);
    await capture(`Run ${index + 1} · ${payload.steps[index].title}`, 'Execution', '.run-lab');
  }
  if (desktop) {
    const mapIDs = await page
      .locator('[data-map]')
      .evaluateAll((nodes) => nodes.map((n) => n.dataset.map));
    for (const id of mapIDs) {
      await click(`[data-map="${id}"]`);
      await capture(`Map diagram · ${id}`, 'System map', '#system-diagram');
      await capture(`Map selection · ${id}`, 'System map', '#map-detail');
    }
  } else {
    await click('[data-map="journal"]');
    await scrollTo('#system-diagram');
    await capture('Map selection · journal', 'System map');
    await capture('Map detail · journal', 'System map', '#map-detail');
  }

  for (const envelope of desktop ? ['xml', 'markdown'] : ['markdown']) {
    await page.selectOption('#format-select', envelope);
    for (const response of ['tool', 'answer', 'invalid']) {
      await click(`[data-response="${response}"]`);
      await capture(`${envelope} · ${response}`, 'Response parsing', '.parser-lab');
    }
  }

  await click('#history-reset');
  for (let revision = 1; revision <= 3; revision += 1) {
    if (revision > 1) await click('#history-compact');
    await capture(`History revision ${revision}`, 'History', '.history-lab');
  }
  await setRange('#history-slider', 1);
  await capture('Original context after two compactions', 'History', '.history-lab');
  for (const id of desktop ? ['A', 'B', 'S1', 'C1', 'S2', 'C2'] : ['B']) {
    await click(`[data-entry="${id}"]`);
    await inspect(`History entry ${id}`);
  }

  for (const mode of ['success', 'lost', 'rejected']) {
    await click(`[data-journal-mode="${mode}"]`);
    const count = mode === 'lost' ? 5 : 4;
    for (let phase = 0; phase < count; phase += 1) {
      if (phase > 0) await click('#journal-next');
      if (desktop || phase >= 3) {
        await capture(`Journal ${mode} · phase ${phase}`, 'Journal', '.journal-lab');
      }
    }
  }

  for (const scenario of ['partial', 'saved', 'tool', 'terminal']) {
    await page.selectOption('#recovery-scenario', scenario);
    const count = scenario === 'terminal' || scenario === 'tool' ? 3 : 4;
    for (let phase = 0; phase < count; phase += 1) {
      if (phase > 0) await click('#recovery-next');
      if (desktop || phase === count - 1) {
        await capture(`Recovery ${scenario} · phase ${phase}`, 'Recovery', '.recovery-lab');
      }
    }
    if (scenario !== 'tool') continue;
    for (const decision of ['completed', 'retry', 'unresolved']) {
      await click('#recovery-reset');
      await click('#recovery-next');
      await click('#recovery-next');
      await click(`[data-tool-recovery="${decision}"]`);
      await capture(`Tool recovery · ${decision}`, 'Recovery', '.recovery-lab');
    }
  }

  await click('#viewer-reset');
  for (let phase = 0; phase < 7; phase += 1) {
    if (phase > 0) await click('#viewer-next');
    if (desktop || [2, 5, 6].includes(phase)) {
      const status = await page.locator('#viewer-status').textContent();
      await capture(`Viewer ${phase} · ${status}`, 'Viewers', '.viewer-lab');
    }
  }

  await click('#stats-reset');
  await click('#stats-fail');
  await click('#stats-fail');
  await capture('Local error gauge · two failures', 'Budgets', '.stats-lab');
  await click('#stats-succeed');
  if (desktop) await capture('Local error gauge · success resets', 'Budgets', '.stats-lab');
  await click('#stats-add-root');
  await click('#stats-add-root');
  await capture('Token limit · exactly 100', 'Budgets', '.stats-lab');
  await click('#stats-add-child');
  await capture('Token limit · root and child stopped', 'Budgets', '.stats-lab');
  await page.selectOption('#stats-scope', 'self');
  if (desktop) await capture('Self limit · independent budgets', 'Budgets', '.stats-lab');
  for (let index = 0; index < 6; index += 1) await click('#stats-add-child');
  await capture('Self limit · only child stopped', 'Budgets', '.stats-lab');

  const notes = page.locator('.chapter > details');
  for (let index = 0; index < (await notes.count()); index += 1) {
    const note = notes.nth(index);
    await note.locator('summary').click();
    const chapter = await note.evaluate((e) => e.closest('.chapter').id);
    await capture(`Expanded note · ${chapter}`, 'Expanded notes', `#${chapter} > details`);
    await note.locator('summary').click();
  }

  for (const recipe of payload.recipes) {
    await click(`[data-recipe="${recipe.id}"]`);
    await capture(`Integration · ${recipe.label}`, 'Recipes', '.recipe-lab');
  }
  await click('#copy-recipe');
  await page.locator('#toast.visible').waitFor();
  await capture('Copy code · feedback', 'Recipes');
  await page.locator('#toast.visible').waitFor({ state: 'hidden' });
  await page.evaluate(() => window.getSelection().removeAllRanges());
  await click('#open-example');
  await inspect('Executable Go example');

  const atlasStates = [
    { title: 'Journal search', query: 'journal', layer: 'all' },
    { title: 'Persistence layer', query: '', layer: 'Persistence' },
    { title: 'Combined search and layer', query: 'journal', layer: 'Persistence' },
    { title: 'No results', query: 'no-such-component', layer: 'Persistence' },
  ];
  for (const state of atlasStates) {
    await page.fill('#atlas-search', state.query);
    await page.selectOption('#atlas-layer', state.layer);
    await scrollTo('#atlas');
    await capture(`Atlas · ${state.title}`, 'Atlas');
  }
  await page.fill('#atlas-search', '');
  await page.selectOption('#atlas-layer', 'all');
  await scrollTo('.atlas-row[data-inspect="trace"]');
  await capture('Atlas · filters while browsing lower entries', 'Atlas');
  const mobileInspectors = [
    'journal',
    'checkpoint',
    'toolresult',
    'context',
    'toolrecovery',
    'trace',
  ];
  for (const component of payload.components) {
    if (!desktop && !mobileInspectors.includes(component.id)) continue;
    await click(`.atlas-row[data-inspect="${component.id}"]`);
    await inspect(`Atlas · ${component.name}`);
  }

  for (let index = 0; index < payload.questions.length; index += 1) {
    const answer = payload.questions[index].answer;
    await click(`[data-question="${index}"][data-option="${(answer + 1) % 3}"]`);
  }
  await capture('Quiz · all corrective feedback', 'Quiz', '#quiz');
  for (const [index, question] of payload.questions.entries()) {
    await click(`[data-question="${index}"][data-option="${question.answer}"]`);
  }
  await capture('Quiz · all answers connected', 'Quiz', '#check');

  await click('#open-colophon');
  await capture('About this edition', 'Colophon', '#detail-dialog');
  const licenses = page.locator('#detail-dialog details');
  for (let index = 0; index < (await licenses.count()); index += 1) {
    const license = licenses.nth(index);
    await license.locator('summary').click();
    await license.scrollIntoViewIfNeeded();
    await capture(`Typeface license ${index + 1} · opened`, 'Colophon', '#detail-dialog');
    await license
      .locator('pre')
      .evaluate((e) => e.scrollTo({ top: e.scrollHeight, behavior: 'instant' }));
    if (desktop)
      await capture(`Typeface license ${index + 1} · scrolled`, 'Colophon', '#detail-dialog');
    await license.locator('summary').click();
  }
  await page.keyboard.press('Escape');
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }));
  await page.keyboard.press('/');
  await scrollTo('#atlas');
  await capture('Keyboard shortcut · search focused', 'Keyboard');
  await setRange('#run-slider', 0);
  await page.clock.install();
  await click('#run-play');
  await page.clock.fastForward(9100);
  await capture('Playback · running', 'Motion', '.run-lab');
  await click('#motion-toggle');
  await capture('Playback · motion paused', 'Motion', '.run-lab');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }));
  await capture('Reduced motion · landing screen', 'Motion');
}
