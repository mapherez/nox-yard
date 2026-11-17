export const browserFixtures = [
  "smoke-history.cjs",
  "smoke-recreate.cjs",
  "smoke-schedules.cjs",
  "smoke-sessions.cjs",
  "smoke-configuration.cjs",
  "smoke-dashboard.cjs",
  "smoke-drawers.cjs",
  "smoke-machine.cjs",
  "smoke-interface-updates.cjs",
  "smoke-compose-parity.cjs",
];

export function isolatedBrowserFixture(fixture) {
  // Fixtures are JavaScript statements, including comments and trailing semicolons.
  return `async (originalPage) => {
    const context = await originalPage.context().browser().newContext({ viewport: { width: 1440, height: 1000 } });
    try {
      const runFixture = ${fixture}
      await runFixture(await context.newPage());
    } finally { await context.close(); }
  }`;
}
