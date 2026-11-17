import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { Script } from "node:vm";
import test from "node:test";
import { browserFixtures, isolatedBrowserFixture } from "./browser-fixtures.mjs";

void test("every actual browser fixture composes as valid JavaScript after formatting", () => {
  for (const name of browserFixtures) {
    const fixture = readFileSync(new URL(name, import.meta.url), "utf8");
    assert.doesNotThrow(
      () => new Script(`(${isolatedBrowserFixture(fixture)})`, { filename: name }),
    );
  }
});

function browser() {
  const page = {};
  let closed = false;
  const context = {
    newPage: async () => page,
    close: async () => {
      closed = true;
    },
  };
  const original = { context: () => ({ browser: () => ({ newContext: async () => context }) }) };
  return { original, page, isClosed: () => closed };
}

void test("comments and optional trailing semicolons do not change fixture execution", async () => {
  for (const ending of ["", ";", ";\n// a trailing comment\n"]) {
    const fixture = `/* leading comment */\nasync (page) => { page.visited = true; }${ending}`;
    const run = new Script(`(${isolatedBrowserFixture(fixture)})`).runInNewContext();
    const fake = browser();
    await run(fake.original);
    assert.equal(fake.page.visited, true);
    assert.equal(fake.isClosed(), true);
  }
});

void test("a failing fixture still closes its isolated browser context", async () => {
  const run = new Script(
    `(${isolatedBrowserFixture('async () => { throw new Error("fixture failed"); };')})`,
  ).runInNewContext();
  const fake = browser();
  await assert.rejects(run(fake.original), /fixture failed/);
  assert.equal(fake.isClosed(), true);
});
