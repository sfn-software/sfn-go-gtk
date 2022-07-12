Siphon
======

Utility to send/receive files via direct connection, written in Go and GTK-3.

![image](https://raw.github.com/solkin/siphon-gtk/master/art/main.png)


Build instructions
------------------

Requirements:

* Go 1.11+
* GTK3 headers (Debian package is called `libgtk-3-dev`)

Assuming you have all that, type:

```
go build
```

This will produce `siphon-gtk` executable in the project folder.

Initial build of GTK bindings will take ~10 minutes,
subsequent builds will be very fast, as you'd expect.
