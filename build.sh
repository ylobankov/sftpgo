#!/bin/bash

set -ex

if [ -z ${GO_VERSION} ] || [ -z ${SFTPGO_VERSION} ] || [ -z ${COMMIT} ]; then
    echo "Please set GO_VERSION, SFTPGO_VERSION, COMMIT env variables!"
    exit 1
fi

apt-get update -q -y
apt-get install -q -y curl gcc

curl --retry 5 --retry-delay 2 --connect-timeout 10 -o go.tar.gz -L https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
tar -C /usr/local -xzf go.tar.gz

export PATH=$PATH:/usr/local/go/bin

go version

cd /usr/local/src
go build -buildvcs=false -trimpath -tags nopgxregisterdefaulttypes -ldflags "-s -w -X github.com/ylobankov/sftpgo/v2/internal/version.commit=${COMMIT} -X github.com/ylobankov/sftpgo/v2/internal/version.date=`date -u +%FT%TZ`" -o sftpgo
