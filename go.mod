module github.com/zrepl/zrepl

go 1.12

require (
	github.com/fatih/color v1.7.0
	github.com/gdamore/tcell v1.2.0
	github.com/go-logfmt/logfmt v0.4.0
	github.com/go-sql-driver/mysql v1.4.1-0.20190907122137-b2c03bcae3d4
	github.com/golang/protobuf v1.3.2
	github.com/google/uuid v1.1.1
	github.com/jinzhu/copier v0.0.0-20170922082739-db4671f3a9b8
	github.com/kr/pretty v0.1.0
	github.com/lib/pq v1.2.0
	github.com/mattn/go-colorable v0.1.4 // indirect
	github.com/mattn/go-isatty v0.0.8
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // // go1.12 mod tidy adds this dependency as 'indirect', but go1.13 mod tidy removes it if the trailing comment is 'indirect' => add this comment to make the build work without changing go.mod on both go1.12 and go1.13
	github.com/modern-go/reflect2 v1.0.1 // go1.12 mod tidy adds this dependency as 'indirect', but go1.13 mod tidy removes it if the trailing comment is 'indirect' => add this comment to make the build work without changing go.mod on both go1.12 and go1.13
	github.com/montanaflynn/stats v0.5.0
	github.com/onsi/ginkgo v1.10.2 // indirect
	github.com/onsi/gomega v1.7.0 // indirect
	github.com/pkg/errors v0.8.1
	github.com/pkg/profile v1.2.1
	github.com/problame/go-netssh v0.0.0-20191209123953-18d8aa6923c7
	github.com/prometheus/client_golang v1.2.1
	github.com/sergi/go-diff v1.0.1-0.20180205163309-da645544ed44 // go1.12 thinks it needs this
	github.com/spf13/cobra v0.0.2
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.4.0
	github.com/yudai/gojsondiff v0.0.0-20170107030110-7b1b7adf999d
	github.com/yudai/golcs v0.0.0-20170316035057-ecda9a501e82 // go1.12 thinks it needs this
	github.com/zrepl/yaml-config v0.0.0-20190928121844-af7ca3f8448f
	golang.org/x/net v0.0.0-20190613194153-d28f0bde5980
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20191026070338-33540a1f6037
	google.golang.org/grpc v1.17.0
)
