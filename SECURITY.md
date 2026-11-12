# Security reporting

Please report suspected vulnerabilities privately through this repository's [private vulnerability reporting](https://github.com/The-IT-Dept/rfspager/security/advisories/new) (Security tab → "Report a vulnerability"). If that is unavailable, email hello@rfspager.app with "Security" in the subject and we will move the discussion to a private advisory. Do not open a public issue or pull request containing exploit details.

Include the affected version or commit, a minimal reproduction using synthetic data, and the expected impact. Do not include live credentials, customer data, raw dispatch logs, or destructive proof-of-concept requests.

## Scope

In scope: the Go server, receiver and tools in this repository, the web application (`apps/web`), the mobile application (`apps/mobile`) and the receiver dashboard (`apps/receiver-ui`), as of the latest release on `main`. Fixes are made on `main` and shipped in the next release; older releases are not maintained.

Do not test against the hosted service at rfspager.app or against anyone's receiver without the operator's explicit permission. Do not create accounts, purchases or receiver registrations on the hosted service to probe it. Local builds with the disposable development database (see README.md) are the place for reproductions.

## Operating your own deployment

Keep keys and provider credentials outside Git, use HTTPS, restrict Maps and other client-side provider keys to your own app identifiers, and configure verified webhook senders (Apple App Store Server Notifications, Google Pub/Sub OIDC, Stripe signing secret). Report a credential leak immediately through the private channel. Rotate exposed credentials; deleting Git content does not revoke a key.
