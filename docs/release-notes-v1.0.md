# wddl 1.0: container image note

The final image now uses Alpine 3.22 instead of `scratch` to provide `/bin/sh`
and BusyBox diagnostics. In the local ARM64 comparison made on 2026-09-13,
the image grew from 7,264,000 bytes to 7,970,896 bytes: an increase of 706,896
bytes (about 9.7%). The exact compressed registry size can vary by platform and
build provenance.

The additional runtime surface is constrained by the default non-root
`10001:10001` user and the recommended Compose settings: read-only root,
all Linux capabilities dropped, `no-new-privileges`, a small `/tmp` tmpfs, and
no published control port. Image history contains only the neutral
`WDDL_CONFIG` value; WebDAV credentials remain runtime-only secrets.
