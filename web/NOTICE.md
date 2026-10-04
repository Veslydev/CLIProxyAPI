# Third-party source

The frontend source, UI components, styles, branding, charts and original pages
were adapted from Seakee/CPA-Manager-Plus, commit
`05ebb7f275dbe575211cb886436d4b99936c1cd9` (MIT).
See `LICENSE.CPA-Manager-Plus` for the required copyright and license notice.

`src/controlplane` supplies the fork's same-origin API integration and pages.
The original manager-service integration is not used by the production entrypoint.
Routing and warm-up behavior was studied from Soju06/codex-lb at
`f8ffbac2099a113fba54dfd8d77774f5bca80ffa`; no Python service is required.
