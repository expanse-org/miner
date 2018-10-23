/*

  Copyright 2017 Loopring Project Ltd (Loopring Foundation).

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.

*/

package node

import (
	"github.com/expanse-org/miner/config"
	"github.com/expanse-org/miner/dao"
	"github.com/expanse-org/miner/datasource"
	"github.com/expanse-org/miner/miner"
	"github.com/expanse-org/miner/miner/timing_matcher"
	"github.com/expanse-org/relay-lib/cache"
	"github.com/expanse-org/relay-lib/cloudwatch"
	"github.com/expanse-org/relay-lib/crypto"
	relayLibEth "github.com/expanse-org/relay-lib/eth"
	"github.com/expanse-org/relay-lib/eth/gasprice_evaluator"
	"github.com/expanse-org/relay-lib/extractor"
	"github.com/expanse-org/relay-lib/log"
	"github.com/expanse-org/relay-lib/marketcap"
	"github.com/expanse-org/relay-lib/marketutil"
	"github.com/expanse-org/relay-lib/sns"
	"github.com/expanse-org/relay-lib/zklock"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"sync"
)

type Node struct {
	globalConfig      *config.GlobalConfig
	rdsService        dao.RdsServiceImpl
	marketCapProvider marketcap.MarketCapProvider
	miner             *miner.Miner

	stop chan struct{}
	lock sync.RWMutex
}

func NewNode(globalConfig *config.GlobalConfig) *Node {
	n := &Node{}
	n.globalConfig = globalConfig
	// register
	n.registerMysql()
	n.registerSnsNotifier()
	cache.NewCache(n.globalConfig.Redis)
	marketutil.Initialize(&n.globalConfig.MarketUtil)
	n.registerMarketCap()
	n.registerAccessor()
	ks := keystore.NewKeyStore(n.globalConfig.Keystore.Keydir, keystore.StandardScryptN, keystore.StandardScryptP)
	n.registerCrypto(ks)
	if _, err := zklock.Initialize(globalConfig.ZkLock); nil != err {
		log.Fatalf("err:%s", err.Error())
	}

	if err := cloudwatch.Initialize(n.globalConfig.CloudWatch); nil != err {
		log.Errorf("err:%s", err.Error())
	}
	datasource.Initialize(globalConfig.DataSource, globalConfig.Mysql, n.marketCapProvider)
	n.registerMiner()
	return n
}

func (n *Node) Start() {
	gasprice_evaluator.InitGasPriceEvaluator()
	n.marketCapProvider.Start()
	n.miner.Start()
	n.registerExtractor()
}

func (n *Node) Wait() {
	n.lock.RLock()

	// TODO(fk): states should be judged

	stop := n.stop
	n.lock.RUnlock()

	<-stop
}

func (n *Node) Stop() {
	n.lock.RLock()
	n.miner.Stop()
	n.lock.RUnlock()
}

func (n *Node) registerCrypto(ks *keystore.KeyStore) {
	c := crypto.NewKSCrypto(true, ks)
	crypto.Initialize(c)
}

func (n *Node) registerMysql() {
	n.rdsService = dao.NewRdsService(n.globalConfig.Mysql)
}

func (n *Node) registerAccessor() {
	if err := relayLibEth.InitializeAccessor(n.globalConfig.Accessor, n.globalConfig.LoopringAccessor); nil != err {
		log.Fatalf("err:%s", err.Error())
	}
}

func (n *Node) registerDataSource() {
	datasource.Initialize(n.globalConfig.DataSource, n.globalConfig.Mysql, n.marketCapProvider)
}

func (n *Node) registerMiner() {
	evaluator := miner.NewEvaluator(n.marketCapProvider, n.globalConfig.Miner)
	submitter, err := miner.NewSubmitter(n.globalConfig.Miner, evaluator, n.rdsService, n.globalConfig.Kafka.Brokers)
	if nil != err {
		log.Fatalf("failed to init submitter, error:%s", err.Error())
	}
	matcher := timing_matcher.NewTimingMatcher(n.globalConfig.Miner.TimingMatcher, submitter, evaluator, n.rdsService, n.marketCapProvider, n.globalConfig.Kafka)
	evaluator.SetMatcher(matcher)
	n.miner = miner.NewMiner(submitter, matcher, evaluator, n.marketCapProvider)
}

func (n *Node) registerMarketCap() {
	n.marketCapProvider = marketcap.NewMarketCapProvider(&n.globalConfig.MarketCap)
}

func (n *Node) registerExtractor() {
	if err := extractor.Initialize(n.globalConfig.Kafka, getKafkaGroup()); err != nil {
		log.Fatalf("node start, register extractor error:%s", err.Error())
	}
}

func (n *Node) registerSnsNotifier() {
	sns.Initialize(n.globalConfig.Sns)
}

func getKafkaGroup() string {
	return "miner_"
}
