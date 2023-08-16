#!/bin/bash

set -ex 

GO_VERSION=1.20.5

SFTPGO_VERSION=$(git describe --tags)
COMMIT=$(git log --oneline -1 --pretty=format:"%h")

docker run \
    --rm \
    --name sftpgo-make-packages \
    --mount type=bind,source=`pwd`,target=/usr/local/src \
    --env GO_VERSION=${GO_VERSION} \
    --env SFTPGO_VERSION=${SFTPGO_VERSION} \
    --env COMMIT=${COMMIT} \
    ubuntu:18.04 /usr/local/src/build.sh

mkdir -p output/{init,sqlite,bash_completion,zsh_completion}

echo "For documentation please take a look here:" > output/README.txt
echo "" >> output/README.txt
echo "https://github.com/drakkan/sftpgo/blob/${SFTPGO_VERSION}/README.md" >> output/README.txt

cp LICENSE output/
cp sftpgo.json output/
cp -r templates output/
cp -r static output/
cp -r openapi output/
cp init/sftpgo.service output/init/

./sftpgo initprovider
./sftpgo gen completion bash > output/bash_completion/sftpgo
./sftpgo gen completion zsh > output/zsh_completion/_sftpgo
./sftpgo gen man -d output/man/man1

gzip output/man/man1/*
cp sftpgo output/
cp sftpgo.db output/sqlite/

cd output
tar cJvf sftpgo_${SFTPGO_VERSION}_linux_amd64.tar.xz *
cd ..

export NFPM_ARCH=amd64
cd pkgs
./build.sh

set +x

echo ""
echo "Packages:"
echo ""
echo "    $(realpath dist/deb/*)"
echo "    $(realpath dist/rpm/*)"
