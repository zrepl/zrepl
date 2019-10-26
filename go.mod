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
	github.com/problame/go-netssh v0.0.0-20191026123024-f34099f4f6b1
	github.com/prometheus/client_golang v1.2.1
	github.com/sergi/go-diff v1.0.1-0.20180205163309-da645544ed44 // go1.12 thinks it needs this
	github.com/spf13/cobra v0.0.2
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.4.0
	github.com/yudai/gojsondiff v0.0.0-20170107030110-7b1b7adf999d
	github.com/yudai/golcs v0.0.0-20170316035057-ecda9a501e82 // go1.12 thinks it needs this
	github.com/zrepl/yaml-config v0.0.0-20190928121844-af7ca3f8448f
	golang.org/x/net v0.0.0-20190613194153-d28f0bde5980
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20191010194322-b09406accb47
	google.golang.org/grpc v1.17.0
)

// invalid dates in transitive dependencies (first validated in Go 1.13, didn't fail in earlier Go versions)
replace (
	github.com/go-critic/go-critic v0.0.0-20181204210945-1df300866540 => github.com/go-critic/go-critic v0.3.5-0.20190526074819-1df300866540
	github.com/go-critic/go-critic v0.0.0-20181204210945-ee9bf5809ead => github.com/go-critic/go-critic v0.3.5-0.20190210220443-ee9bf5809ead
	github.com/golangci/errcheck v0.0.0-20181003203344-ef45e06d44b6 => github.com/golangci/errcheck v0.0.0-20181223084120-ef45e06d44b6
	github.com/golangci/go-tools v0.0.0-20180109140146-35a9f45a5db0 => github.com/golangci/go-tools v0.0.0-20190124090046-35a9f45a5db0
	github.com/golangci/go-tools v0.0.0-20180109140146-af6baa5dc196 => github.com/golangci/go-tools v0.0.0-20190318060251-af6baa5dc196
	github.com/golangci/gofmt v0.0.0-20181105071733-0b8337e80d98 => github.com/golangci/gofmt v0.0.0-20181222123516-0b8337e80d98
	github.com/golangci/gosec v0.0.0-20180901114220-66fb7fc33547 => github.com/golangci/gosec v0.0.0-20190211064107-66fb7fc33547
	github.com/golangci/ineffassign v0.0.0-20180808204949-42439a7714cc => github.com/golangci/ineffassign v0.0.0-20190609212857-42439a7714cc
	github.com/golangci/lint-1 v0.0.0-20180610141402-ee948d087217 => github.com/golangci/lint-1 v0.0.0-20190420132249-ee948d087217
	golang.org/x/tools v0.0.0-20190125232054-379209517ffe => golang.org/x/tools v0.0.0-20190205201329-379209517ffe
	mvdan.cc/unparam v0.0.0-20190124213536-fbb59629db34 => mvdan.cc/unparam v0.0.0-20190209190245-fbb59629db34
)
