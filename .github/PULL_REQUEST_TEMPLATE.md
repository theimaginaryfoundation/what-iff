## Summary

<!-- What changed and why, in 1-3 bullets. -->

## Test plan

<!-- Commands you ran or steps you took to verify this. -->

- [ ] `make pre-commit` (fmt, vet, tidy, test, build)
- [ ] Frontend lint/test/build (only if `web/` changed)
- [ ] Manually verified: <!-- what, briefly -->

## Checklist

- [ ] No credentials, personal exports, generated local state, or production config committed
- [ ] Public defaults still work without hosted services (local JWT auth, unlimited usage)
- [ ] Updated `docs/ARCHITECTURE_SUMMARY.md` / the affected `_PACKAGE_SUMMARY.md` if this touches system boundaries
- [ ] Regenerated the e2e SDK (`cd web/app && npm run sdk:generate`) if `openapi.yaml` changed
