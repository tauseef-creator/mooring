# portman

See which process and project is using each port on your machine, and stop it with one command.

```
$ portman
PORT   PID     COMMAND       PROJECT          UPTIME
3000   48213   npm run dev   shop-frontend    2h 14m
```

Linux only for now.

## Build

```
go build -o portman .
```
