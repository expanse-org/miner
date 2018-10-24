#!/bin/bash
#AfterInstall

WORK_DIR=/opt/loopring/miner
SVC_DIR=/etc/service/miner
GOROOT=/usr/lib/go-1.9
export PATH=$PATH:$GOROOT/bin
export GOPATH=/opt/loopring/go-src

#cp svc config to svc if this node is not miner
if [ ! -d /opt/loopring/miner ]; then
    sudo cp -rf $WORK_DIR/src/bin/svc/* $SVC_DIR
    sudo chmod -R 755 $SVC_DIR
fi

SRC_DIR=$GOPATH/src/github.com/expanse-org/miner
if [ ! -d $SRC_DIR ]; then
      sudo mkdir -p $SRC_DIR
	  sudo chown -R ubuntu:ubuntu $GOPATH
fi

cd $SRC_DIR
rm -rf ./*
cp -r $WORK_DIR/src/* ./
go build -ldflags -s -v  -o build/bin/miner cmd/lrc/*
cp build/bin/miner $WORK_DIR/bin/
