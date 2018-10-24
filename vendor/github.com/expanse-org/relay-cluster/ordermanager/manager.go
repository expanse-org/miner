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

package ordermanager

import (
	"github.com/expanse-org/relay-cluster/dao"
	notify "github.com/expanse-org/relay-cluster/util"
	"github.com/expanse-org/relay-lib/eventemitter"
	"github.com/expanse-org/relay-lib/log"
	"github.com/expanse-org/relay-lib/marketcap"
	util "github.com/expanse-org/relay-lib/marketutil"
	"github.com/expanse-org/relay-lib/types"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type OrderManager interface {
	Start()
	Stop()
}

type OrderManagerImpl struct {
	options                 *OrderManagerOptions
	rds                     *dao.RdsService
	processor               *ForkProcessor
	cutoffCache             *CutoffCache
	mc                      marketcap.MarketCapProvider
	newOrderWatcher         *eventemitter.Watcher
	ringMinedWatcher        *eventemitter.Watcher
	fillOrderWatcher        *eventemitter.Watcher
	cancelOrderWatcher      *eventemitter.Watcher
	cutoffOrderWatcher      *eventemitter.Watcher
	cutoffPairWatcher       *eventemitter.Watcher
	forkWatcher             *eventemitter.Watcher
	warningWatcher          *eventemitter.Watcher
	submitRingMethodWatcher *eventemitter.Watcher
}

type OrderManagerOptions struct {
	CutoffCacheExpireTime int64
	CutoffCacheCleanTime  int64
}

func NewOrderManager(
	options *OrderManagerOptions,
	rds *dao.RdsService,
	market marketcap.MarketCapProvider) *OrderManagerImpl {

	om := &OrderManagerImpl{}
	om.options = options
	om.rds = rds
	om.processor = NewForkProcess(om.rds, market)
	om.mc = market
	om.cutoffCache = NewCutoffCache(options.CutoffCacheCleanTime)

	return om
}

// Start start orderbook as a service
func (om *OrderManagerImpl) Start() {
	om.newOrderWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleGatewayOrder}
	om.ringMinedWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleRingMined}
	om.fillOrderWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleOrderFilled}
	om.cancelOrderWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleOrderCancelled}
	om.cutoffOrderWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleCutoff}
	om.cutoffPairWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleCutoffPair}
	om.forkWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleFork}
	om.warningWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleWarning}
	om.submitRingMethodWatcher = &eventemitter.Watcher{Concurrent: false, Handle: om.handleSubmitRingMethod}

	eventemitter.On(eventemitter.NewOrder, om.newOrderWatcher)
	eventemitter.On(eventemitter.RingMined, om.ringMinedWatcher)
	eventemitter.On(eventemitter.OrderFilled, om.fillOrderWatcher)
	eventemitter.On(eventemitter.CancelOrder, om.cancelOrderWatcher)
	eventemitter.On(eventemitter.CutoffAll, om.cutoffOrderWatcher)
	eventemitter.On(eventemitter.CutoffPair, om.cutoffPairWatcher)
	eventemitter.On(eventemitter.ChainForkDetected, om.forkWatcher)
	eventemitter.On(eventemitter.ExtractorWarning, om.warningWatcher)
	eventemitter.On(eventemitter.Miner_SubmitRing_Method, om.submitRingMethodWatcher)
}

func (om *OrderManagerImpl) Stop() {
	eventemitter.Un(eventemitter.NewOrder, om.newOrderWatcher)
	eventemitter.Un(eventemitter.RingMined, om.ringMinedWatcher)
	eventemitter.Un(eventemitter.OrderFilled, om.fillOrderWatcher)
	eventemitter.Un(eventemitter.CancelOrder, om.cancelOrderWatcher)
	eventemitter.Un(eventemitter.CutoffAll, om.cutoffOrderWatcher)
	eventemitter.Un(eventemitter.ChainForkDetected, om.forkWatcher)
	eventemitter.Un(eventemitter.ExtractorWarning, om.warningWatcher)
	eventemitter.Un(eventemitter.Miner_SubmitRing_Method, om.submitRingMethodWatcher)
}

func (om *OrderManagerImpl) handleFork(input eventemitter.EventData) error {
	log.Debugf("order manager processing chain fork......")

	om.Stop()
	if err := om.processor.Fork(input.(*types.ForkedEvent)); err != nil {
		log.Fatalf("order manager,handle fork error:%s", err.Error())
	}
	om.Start()

	return nil
}

func (om *OrderManagerImpl) handleWarning(input eventemitter.EventData) error {
	log.Debugf("order manager processing extractor warning")
	om.Stop()
	return nil
}

func (om *OrderManagerImpl) handleSubmitRingMethod(input eventemitter.EventData) error {
	event := input.(*types.SubmitRingMethodEvent)

	if event.Status != types.TX_STATUS_FAILED {
		return nil
	}

	var (
		model = &dao.RingMinedEvent{}
		err   error
	)

	model, err = om.rds.FindRingMined(event.TxHash.Hex())
	if err == nil {
		log.Debugf("order manager,handle submitRing method,tx %s has already exist", event.TxHash.Hex())
		return nil
	}
	model.FromSubmitRingMethod(event)
	if err = om.rds.Add(model); err != nil {
		log.Debugf("order manager,handle submitRing method,tx:%s insert ring error:%s", event.TxHash.Hex(), err.Error())
		return nil
	}

	log.Debugf("order manager,handle submitRing method,tx:%s status:%s", event.TxHash.Hex(), types.StatusStr(event.Status))

	return nil
}

// 所有来自gateway的订单都是新订单
func (om *OrderManagerImpl) handleGatewayOrder(input eventemitter.EventData) error {
	state := input.(*types.OrderState)

	model, err := newOrderEntity(state, om.mc, nil)
	if err != nil {
		log.Errorf("order manager,handle gateway order:%s error", state.RawOrder.Hash.Hex())
		return err
	}

	if err = om.rds.Add(model); err != nil {
		return err
	}

	log.Debugf("order manager,handle gateway order,order.hash:%s amountS:%s", state.RawOrder.Hash.Hex(), state.RawOrder.AmountS.String())

	notify.NotifyOrderUpdate(state)
	return nil
}

func (om *OrderManagerImpl) handleRingMined(input eventemitter.EventData) error {
	event := input.(*types.RingMinedEvent)

	if event.Status != types.TX_STATUS_SUCCESS {
		return nil
	}

	var (
		model = &dao.RingMinedEvent{}
		err   error
	)

	model, err = om.rds.FindRingMined(event.TxHash.Hex())
	if err == nil {
		log.Debugf("order manager,handle ringmined event,tx:%s ring %s has already exist", event.TxHash.Hex(), event.Ringhash.Hex())
		return nil
	}
	model.ConvertDown(event)
	if err = om.rds.Add(model); err != nil {
		log.Debugf("order manager,handle ringmined event,tx:%s insert ring :%s error:%s", event.TxHash.Hex(), event.Ringhash.Hex(), err.Error())
		return nil
	}

	log.Debugf("order manager,handle ringmined event,tx:%s, ringhash:%s", event.TxHash.Hex(), event.Ringhash.Hex())

	return nil
}

func (om *OrderManagerImpl) handleOrderFilled(input eventemitter.EventData) error {
	event := input.(*types.OrderFilledEvent)

	if event.Status != types.TX_STATUS_SUCCESS {
		return nil
	}

	// save fill event
	_, err := om.rds.FindFillEvent(event.TxHash.Hex(), event.FillIndex.Int64())
	if err == nil {
		log.Debugf("order manager,handle order filled event tx:%s fillIndex:%d fill already exist", event.TxHash.String(), event.FillIndex)
		return nil
	}

	// get rds.Order and types.OrderState
	state := &types.OrderState{UpdatedBlock: event.BlockNumber}
	model, err := om.rds.GetOrderByHash(event.OrderHash)
	if err != nil {
		return err
	}
	if err := model.ConvertUp(state); err != nil {
		return err
	}

	newFillModel := &dao.FillEvent{}
	newFillModel.ConvertDown(event)
	newFillModel.Fork = false
	newFillModel.OrderType = state.RawOrder.OrderType
	newFillModel.Side = util.GetSide(util.AddressToAlias(event.TokenS.Hex()), util.AddressToAlias(event.TokenB.Hex()))
	newFillModel.Market, _ = util.WrapMarketByAddress(event.TokenB.Hex(), event.TokenS.Hex())

	if err := om.rds.Add(newFillModel); err != nil {
		log.Debugf("order manager,handle order filled event tx:%s fillIndex:%s orderhash:%s error:insert failed",
			event.TxHash.Hex(), event.FillIndex.String(), event.OrderHash.Hex())
		return err
	}

	// judge order status
	if state.Status == types.ORDER_CUTOFF || state.Status == types.ORDER_FINISHED || state.Status == types.ORDER_UNKNOWN {
		log.Debugf("order manager,handle order filled event tx:%s fillIndex:%s orderhash:%s status:%d invalid",
			event.TxHash.Hex(), event.FillIndex.String(), event.OrderHash.Hex(), state.Status)
		return nil
	}

	// calculate dealt amount
	state.UpdatedBlock = event.BlockNumber
	state.DealtAmountS = new(big.Int).Add(state.DealtAmountS, event.AmountS)
	state.DealtAmountB = new(big.Int).Add(state.DealtAmountB, event.AmountB)
	state.SplitAmountS = new(big.Int).Add(state.SplitAmountS, event.SplitS)
	state.SplitAmountB = new(big.Int).Add(state.SplitAmountB, event.SplitB)

	// update order status
	settleOrderStatus(state, om.mc, false)

	// update rds.Order
	if err := model.ConvertDown(state); err != nil {
		log.Errorf(err.Error())
		return err
	}
	if err := om.rds.UpdateOrderWhileFill(state.RawOrder.Hash, state.Status, state.DealtAmountS, state.DealtAmountB, state.SplitAmountS, state.SplitAmountB, state.UpdatedBlock); err != nil {
		return err
	}

	log.Debugf("order manager,handle order filled event tx:%s, fillIndex:%s, orderhash:%s, dealAmountS:%s, dealtAmountB:%s",
		event.TxHash.Hex(), event.FillIndex.String(), state.RawOrder.Hash.Hex(), state.DealtAmountS.String(), state.DealtAmountB.String())

	notify.NotifyOrderFilled(newFillModel)
	return nil
}

func (om *OrderManagerImpl) handleOrderCancelled(input eventemitter.EventData) error {
	event := input.(*types.OrderCancelledEvent)

	if event.Status != types.TX_STATUS_SUCCESS {
		return nil
	}

	// save cancel event
	_, err := om.rds.GetCancelEvent(event.TxHash)
	if err == nil {
		log.Debugf("order manager,handle order cancelled event tx:%s, orderhash:%s error:order have already exist", event.TxHash.Hex(), event.OrderHash.Hex())
		return nil
	}
	newCancelEventModel := &dao.CancelEvent{}
	newCancelEventModel.ConvertDown(event)
	newCancelEventModel.Fork = false
	if err := om.rds.Add(newCancelEventModel); err != nil {
		return err
	}

	// get rds.Order and types.OrderState
	state := &types.OrderState{}
	model, err := om.rds.GetOrderByHash(event.OrderHash)
	if err != nil {
		return err
	}
	if err := model.ConvertUp(state); err != nil {
		return err
	}

	// calculate remainAmount and cancelled amount should be saved whether order is finished or not
	if state.RawOrder.BuyNoMoreThanAmountB {
		state.CancelledAmountB = new(big.Int).Add(state.CancelledAmountB, event.AmountCancelled)
		log.Debugf("order manager,handle order cancelled event tx:%s, order:%s cancelled amountb:%s", event.TxHash.Hex(), state.RawOrder.Hash.Hex(), state.CancelledAmountB.String())
	} else {
		state.CancelledAmountS = new(big.Int).Add(state.CancelledAmountS, event.AmountCancelled)
		log.Debugf("order manager,handle order cancelled event tx:%s, order:%s cancelled amounts:%s", event.TxHash.Hex(), state.RawOrder.Hash.Hex(), state.CancelledAmountS.String())
	}

	// update order status
	settleOrderStatus(state, om.mc, true)
	state.UpdatedBlock = event.BlockNumber

	// update rds.Order
	if err := model.ConvertDown(state); err != nil {
		return err
	}
	if err := om.rds.UpdateOrderWhileCancel(state.RawOrder.Hash, state.Status, state.CancelledAmountS, state.CancelledAmountB, state.UpdatedBlock); err != nil {
		return err
	}

	notify.NotifyOrderUpdate(state)

	return nil
}

// 所有cutoff event都应该存起来,但不是所有event都会影响订单
func (om *OrderManagerImpl) handleCutoff(input eventemitter.EventData) error {
	evt := input.(*types.CutoffEvent)

	if evt.Status != types.TX_STATUS_SUCCESS {
		return nil
	}

	// check tx exist
	_, err := om.rds.GetCutoffEvent(evt.TxHash)
	if err == nil {
		log.Debugf("order manager,handle order cutoff event tx:%s error:transaction have already exist", evt.TxHash.Hex())
		return nil
	}

	lastCutoff := om.cutoffCache.GetCutoff(evt.Protocol, evt.Owner)

	var orderHashList []common.Hash

	// 首次存储到缓存，lastCutoff == currentCutoff
	if evt.Cutoff.Cmp(lastCutoff) < 0 {
		log.Debugf("order manager,handle cutoff event tx:%s, protocol:%s - owner:%s lastCutofftime:%s > currentCutoffTime:%s", evt.TxHash.Hex(), evt.Protocol.Hex(), evt.Owner.Hex(), lastCutoff.String(), evt.Cutoff.String())
	} else {
		om.cutoffCache.UpdateCutoff(evt.Protocol, evt.Owner, evt.Cutoff)
		if orders, _ := om.rds.GetCutoffOrders(evt.Owner, evt.Cutoff); len(orders) > 0 {
			for _, v := range orders {
				var state types.OrderState
				v.ConvertUp(&state)
				orderHashList = append(orderHashList, state.RawOrder.Hash)
			}
			om.rds.SetCutOffOrders(orderHashList, evt.BlockNumber)
		}
		log.Debugf("order manager,handle cutoff event tx:%s, owner:%s, cutoffTimestamp:%s", evt.TxHash.Hex(), evt.Owner.Hex(), evt.Cutoff.String())
	}

	// save cutoff event
	evt.OrderHashList = orderHashList
	newCutoffEventModel := &dao.CutOffEvent{}
	newCutoffEventModel.ConvertDown(evt)
	newCutoffEventModel.Fork = false

	if err := om.rds.Add(newCutoffEventModel); err != nil {
		return err
	}

	notify.NotifyCutoff(evt)

	return nil
}

func (om *OrderManagerImpl) handleCutoffPair(input eventemitter.EventData) error {
	evt := input.(*types.CutoffPairEvent)

	if evt.Status != types.TX_STATUS_SUCCESS {
		return nil
	}

	// check tx exist
	_, err := om.rds.GetCutoffPairEvent(evt.TxHash)
	if err == nil {
		log.Debugf("order manager,handle order cutoffPair event tx:%s error:transaction have already exist", evt.TxHash.Hex())
		return nil
	}

	lastCutoffPair := om.cutoffCache.GetCutoffPair(evt.Protocol, evt.Owner, evt.Token1, evt.Token2)

	var orderHashList []common.Hash
	// 首次存储到缓存，lastCutoffPair == currentCutoffPair
	if evt.Cutoff.Cmp(lastCutoffPair) < 0 {
		log.Debugf("order manager,handle cutoffPair event tx:%s, protocol:%s - owner:%s lastCutoffPairtime:%s > currentCutoffPairTime:%s", evt.TxHash.Hex(), evt.Protocol.Hex(), evt.Owner.Hex(), lastCutoffPair.String(), evt.Cutoff.String())
	} else {
		om.cutoffCache.UpdateCutoffPair(evt.Protocol, evt.Owner, evt.Token1, evt.Token2, evt.Cutoff)
		if orders, _ := om.rds.GetCutoffPairOrders(evt.Owner, evt.Token1, evt.Token2, evt.Cutoff); len(orders) > 0 {
			for _, v := range orders {
				var state types.OrderState
				v.ConvertUp(&state)
				orderHashList = append(orderHashList, state.RawOrder.Hash)
			}
			om.rds.SetCutOffOrders(orderHashList, evt.BlockNumber)
		}
		log.Debugf("order manager,handle cutoffPair event tx:%s, owner:%s, token1:%s, token2:%s, cutoffTimestamp:%s", evt.TxHash.Hex(), evt.Owner.Hex(), evt.Token1.Hex(), evt.Token2.Hex(), evt.Cutoff.String())
	}

	// save transaction
	evt.OrderHashList = orderHashList
	newCutoffPairEventModel := &dao.CutOffPairEvent{}
	newCutoffPairEventModel.ConvertDown(evt)
	newCutoffPairEventModel.Fork = false

	if err := om.rds.Add(newCutoffPairEventModel); err != nil {
		return err
	}

	notify.NotifyCutoffPair(evt)

	return err
}
