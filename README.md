# A bridge between systemd-timesyncd and mDNS

This project closes the gap between mDNS NTP announcements and [systemd-timesyncd](https://www.freedesktop.org/software/systemd/man/systemd-timesyncd.service.html).

Since version 250 of systemd, timesyncd can be taught about servers that come and go at runtime.
This project monitors mDNS service announcements and pushes the current list of discovered services as runtime servers.

# License

MIT
