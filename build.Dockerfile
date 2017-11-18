FROM golang:latest

# Docs deps
RUN apt-get update && apt-get install -y \
    python3-pip

ADD lazy.sh /tmp/lazy.sh

RUN /tmp/lazy.sh builddep

ADD docs/requirements.txt /tmp/requirements.txt
ENV ZREPL_LAZY_DOCS_REQPATH=/tmp/requirements.txt
RUN /tmp/lazy.sh docdep

RUN mkdir -p /go/src/github.com/zrepl
RUN ln -sf /zrepl /go/src/github.com/zrepl/zrepl

WORKDIR /go/src/github.com/zrepl/zrepl


