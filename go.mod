module github.com/zrepl/zrepl

go 1.12

require (
	github.com/OpenPeeDeeP/depguard v0.0.0-20181229194401-1f388ab2d810 // indirect
	github.com/alvaroloes/enumer v1.1.1
	github.com/beorn7/perks v0.0.0-20180321164747-3a771d992973 // indirect
	github.com/fatih/color v1.7.0
	github.com/ftrvxmtrx/fd v0.0.0-20150925145434-c6d800382fff // indirect
	github.com/gdamore/tcell v1.2.0
	github.com/go-critic/go-critic v0.3.4 // indirect
	github.com/go-logfmt/logfmt v0.3.0
	github.com/go-sql-driver/mysql v1.4.1-0.20190907122137-b2c03bcae3d4
	github.com/gogo/protobuf v1.2.1 // indirect
	github.com/golang/mock v1.2.0 // indirect
	github.com/golang/protobuf v1.2.0
	github.com/golangci/errcheck v0.0.0-20181223084120-ef45e06d44b6 // indirect
	github.com/golangci/go-tools v0.0.0-20190124090046-35a9f45a5db0 // indirect
	github.com/golangci/gocyclo v0.0.0-20180528144436-0a533e8fa43d // indirect
	github.com/golangci/gofmt v0.0.0-20181222123516-0b8337e80d98 // indirect
	github.com/golangci/golangci-lint v1.17.1
	github.com/golangci/gosec v0.0.0-20180901114220-8afd9cbb6cfb // indirect
	github.com/golangci/lint-1 v0.0.0-20181222135242-d2cdd8c08219 // indirect
	github.com/golangci/misspell v0.3.4 // indirect
	github.com/golangci/revgrep v0.0.0-20180812185044-276a5c0a1039 // indirect
	github.com/google/uuid v1.1.1
	github.com/jinzhu/copier v0.0.0-20170922082739-db4671f3a9b8
	github.com/k0kubun/colorstring v0.0.0-20150214042306-9440f1994b88 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kr/logfmt v0.0.0-20140226030751-b84e30acd515 // indirect
	github.com/kr/pretty v0.1.0
	github.com/lib/pq v1.2.0
	github.com/mattn/go-isatty v0.0.3
	github.com/matttproud/golang_protobuf_extensions v1.0.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/montanaflynn/stats v0.5.0
	github.com/pkg/errors v0.8.0
	github.com/pkg/profile v1.2.1
	github.com/problame/go-netssh v0.0.0-20190110232351-09d6bc45d284
	github.com/prometheus/client_golang v0.0.0-20180410130117-e11c6ff8170b
	github.com/prometheus/client_model v0.0.0-20171117100541-99fa1f4be8e5 // indirect
	github.com/prometheus/common v0.0.0-20180413074202-d0f7cd64bda4 // indirect
	github.com/prometheus/procfs v0.0.0-20180408092902-8b1c2da0d56d // indirect
	github.com/sergi/go-diff v1.0.0 // indirect
	github.com/sirupsen/logrus v1.4.0 // indirect
	github.com/spf13/afero v1.2.2 // indirect
	github.com/spf13/cobra v0.0.2
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.3
	github.com/spf13/viper v1.3.2 // indirect
	github.com/stretchr/testify v1.4.0
	github.com/theckman/goconstraint v1.11.0 // indirect
	github.com/yudai/gojsondiff v0.0.0-20170107030110-7b1b7adf999d
	github.com/yudai/golcs v0.0.0-20170316035057-ecda9a501e82 // indirect
	github.com/yudai/pp v2.0.1+incompatible // indirect
	github.com/zrepl/yaml-config v0.0.0-20190903123849-809f4603401a
	golang.org/x/crypto v0.0.0-20190325154230-a5d413f7728c // indirect
	golang.org/x/net v0.0.0-20190313220215-9f648a60d977
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20190626150813-e07cf5db2756
	golang.org/x/tools v0.0.0-20190524210228-3d17549cdc6b
	google.golang.org/genproto v0.0.0-20181202183823-bd91e49a0898 // indirect
	google.golang.org/grpc v1.17.0
	mvdan.cc/unparam v0.0.0-20190310220240-1b9ccfa71afe // indirect
	sourcegraph.com/sqs/pbtypes v1.0.0 // indirect
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
