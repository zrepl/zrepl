The tool in this package (`go run . -h`) scrapes log lines produces by the `github.com/zrepl/zrepl/zfs/zfscmd` package
into a stream of JSON objects.

The `analysis.ipynb` then runs some basic analysis on the collected log output.

## Deps for the `scrape_graylog_csv.bash` script

```
pip install --upgrade git+https://github.com/lk-jeffpeck/csvfilter.git@ec433f14330fbbf5d41f56febfeedac22868a949
```

