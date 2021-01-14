A tool that re-used zrepl abstractions to be an rsync-like sync tool for ZFS.

Test environment:

```
go build -o zync && rsync -a ./zync root@192.168.124.233:/usr/local/bin/ && sudo ./zync local:///rpool/zrepltlstest/src ssh://root:%2Fhome%2Fcs%2Fzrepl%2Fzrepl%2Fzync%2Ftestid@192.168.124.233/p1/zync_sink 
```
