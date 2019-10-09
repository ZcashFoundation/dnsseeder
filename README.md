## Zcash Network Crawler

This is a CoreDNS plugin that scrapes addresses of peers from a Zcash network. It's intended as a safer and more scalable replacement for the [zcash-seeder](https://github.com/zcash/zcash-seeder) project.

It uses [btcsuite](https://github.com/btcsuite) for networking and [crawshaw/sqlite](https://crawshaw.io/blog/go-and-sqlite) for storing peer data.
