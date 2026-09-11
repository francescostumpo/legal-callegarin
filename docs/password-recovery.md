# Administrator password recovery

Password recovery is an operator procedure. The application does not send reset email and does not accept a plaintext password through configuration.

1. On a trusted workstation, run `go run ./cmd/adminhash` with no arguments. Enter the new password twice at the hidden prompts. For automation, provide exactly two newline-terminated copies on standard input. Treat the single PHC line on standard output as a secret and do not paste the plaintext password into a shell command, log, ticket, or chat.
2. Put the generated PHC value into the protected deployment secret that supplies `ADMIN_PASSWORD_HASH`. Use the approved CI protected environment or Azure secret-input workflow; do not place the value in Bicep parameter files, repository files, command arguments, or deployment logs.
3. Update the Container App secret reference and create a new revision through the normal reviewed deployment path. Confirm the new revision is healthy before moving traffic and retain the previous healthy revision for rollback.
4. Replacing the PHC changes the application's credential-version digest. Every session created with the prior hash is rejected immediately, even if its eight-hour expiry has not elapsed. Expired and revoked session rows may then be removed by the normal session cleanup operation.
5. In a private browser window, verify that the prior session is rejected, sign in with the new password, call the session endpoint successfully, and log out. Confirm that the logout revokes the session and that no credential, raw cookie, or PHC value appears in application or deployment logs.

If deployment or verification is ambiguous, keep traffic on the last known healthy revision and repeat the secret update through the approved path. Never restore the prior password merely to make an uncertain deployment appear successful.
