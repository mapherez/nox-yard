import { readFileSync } from 'node:fs';
import { checkReleaseVersion, isMain } from './release-version.mjs';

if (isMain(import.meta.url)) {
  try {
    if (process.argv.length !== 3) throw new Error('Usage: node scripts/check-release-version.mjs <tag>');
    const release = checkReleaseVersion(process.argv[2], readFileSync('VERSION'));
    console.log(`Validated ${release.tag} against VERSION.`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
