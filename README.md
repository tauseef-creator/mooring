# Mooring

See who's tied up on your ports: which process, which project, and for how long.

```
$ mooring
PORT   PID     USER   COMMAND       PROJECT         UPTIME   ADDRESS
3000   48213   me     npm run dev   shop-frontend   2h 14m   127.0.0.1
8765   51022   me     python3 -m…   ~/Downloads     5m       0.0.0.0
```

Linux only for now. Mooring reads `/proc` directly and needs no other tools.

## Build

```
go build -o mooring .
```

## Environment

| Variable    | Default | Purpose                                 |
|-------------|---------|-----------------------------------------|
| `HOST_PROC` | `/proc` | procfs mount point                      |
| `HOST_ROOT` | `/`     | filesystem root for finding projects    |
