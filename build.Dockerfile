FROM golang:latest

RUN apt-get update && apt-get install -y \
    python3-pip

ADD lazy.sh /tmp/lazy.sh
ADD docs/requirements.txt /tmp/requirements.txt
ENV ZREPL_LAZY_DOCS_REQPATH=/tmp/requirements.txt
RUN /tmp/lazy.sh devsetup

# prepare volume mount of git checkout to /zrepl
RUN mkdir -p /go/src/github.com/zrepl
RUN chmod -R 0777 /go
WORKDIR /go/src/github.com/zrepl/zrepl


