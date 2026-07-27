# Generated demo evidence

Attesta writes local CI/CD and failure-mode evidence beneath this directory. Run outputs are intentionally ignored because they contain timestamps, environment-specific paths, raw logs, and rendered manifests that create repository churn.

Generate fresh evidence with:

```bash
make demo-full
make demo-failures
```

For a release or public benchmark, publish a curated, scrubbed evidence bundle as a GitHub release asset and document its source revision, tool versions, configuration, and verification steps.
