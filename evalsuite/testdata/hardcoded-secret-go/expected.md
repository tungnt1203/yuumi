# Expected: hardcoded-secret-go

The PR adds `AccessKeyID`/`SecretAccessKey` to `Config` but assigns AWS
credentials as string literals in code, instead of reading them from
environment variables like `Bucket`/`Region` right next to them. The
credentials end up in git history (issue #64). The values in this fixture
are the public example keys from the AWS documentation, not real keys.

The bot MUST catch:

- [ ] A `category: "security"`, `severity: "critical"` (`"high"` accepted)
      finding about hardcoded credentials/secrets.
- [ ] The finding is on `storage.go`, at the `AccessKeyID:` or
      `SecretAccessKey:` line.
- [ ] The `suggestion` (if any) reads from environment variables
      (`os.Getenv`) or a secret manager, and does not copy the key values
      into the message/suggestion.

It should not report false positives on `Bucket`/`Region`: those fields
already come from environment variables and are unchanged in this PR.
