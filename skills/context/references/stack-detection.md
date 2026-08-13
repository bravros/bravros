# Stack Detection Reference

Detect from the files the model already knows how to read — this file only pins the expected output fields.

1. **Language** — root manifest (`composer.json`, `package.json`, `go.mod`, `Gemfile`, `pyproject.toml`/`requirements.txt`, `Cargo.toml`, `pom.xml`/`build.gradle`, `*.csproj`, `mix.exs`, `pubspec.yaml`). Multiple hits → polyglot, note all.
2. **Framework + exact version** — read the LOCK file (`composer.lock`, `package-lock.json`/`yarn.lock`/`pnpm-lock.yaml`, `go.sum`, `Gemfile.lock`), not the manifest, so versions are exact.
3. **Test runner** — dev-dependency markers + config files (see `test-runners.md`).
4. **Asset pipeline** — `vite.config.*`, `webpack.config.*`, `tailwind.config.*`, `postcss.config.*`, esbuild in package.json.
5. **Database** — `.env` / `.env.example` (`DB_CONNECTION`, `DATABASE_URL`, `MONGODB_URI`, `REDIS_HOST`) and driver dependencies.

Report a compact summary: language + version, framework(s) + version, test runner, assets, database(s).
