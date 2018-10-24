
# vendor init
rm -rf $GOPATH/src/github.com/expanse-org/miner/vendor
govendor init

# vendor add external libraries
govendor add +external

# copy go-ethrenum c libs
rm -rf $GOPATH/src/github.com/expanse-org/miner/vendor/github.com/ethereum/go-ethereum/crypto/secp256k1
cp -r $GOPATH/src/github.com/ethereum/go-ethereum/crypto/secp256k1 $GOPATH/src/github.com/expanse-org/miner/vendor/github.com/ethereum/go-ethereum/crypto/
