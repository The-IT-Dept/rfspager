# Security reporting

Please report suspected vulnerabilities using this repository's private vulnerability reporting facility (the Security tab). Until that facility is enabled for the public repository, contact the maintainer through an existing private support channel; do not open a public issue containing exploit details. Include the affected version/commit, a minimal reproduction using synthetic data, and the expected impact. Do not include live credentials, customer data, raw dispatch logs, or destructive proof-of-concept requests.

Do not test against hosted RFSPager services or receivers without the operator's explicit permission. Local builds and disposable test databases are available for reproductions.

The project is preparing its Laravel-to-Go cutover. Security fixes target the Go replacement in this repository; the running Laravel service is a separate deployment until the operator completes the cutover. No historical version is promised continuing maintenance.

Deployment operators should keep their keys and provider credentials outside Git, use HTTPS, restrict Maps/client-provider keys to their own app, and configure verified webhook senders. Report a credential leak immediately through the private channel. Rotate exposed credentials before any publication; deleting Git content does not revoke a key.
