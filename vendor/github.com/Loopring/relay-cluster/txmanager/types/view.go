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

package types

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	util "github.com/expanse-org/relay-lib/marketutil"
	"github.com/expanse-org/relay-lib/types"
)

type TransactionView struct {
	Symbol      string         `json:"symbol"`
	Owner       common.Address `json:"owner"` // 用户地址
	TxHash      common.Hash    `json:"tx_hash"`
	BlockNumber int64          `json:"block_number"`
	LogIndex    int64          `json:"log_index"`
	Amount      *big.Int       `json:"amount"`
	Nonce       *big.Int       `json:"nonce"`
	Type        TxType         `json:"type"`
	Status      types.TxStatus `json:"status"`
	CreateTime  int64          `json:"create_time"`
	UpdateTime  int64          `json:"update_time"`
}

func ApproveView(src *types.ApprovalEvent) (TransactionView, error) {
	var (
		tx  TransactionView
		err error
	)

	if tx.Symbol, err = util.GetSymbolWithAddress(src.Protocol); err != nil {
		return tx, err
	}
	if err = tx.fullFilled(src.TxInfo); err != nil {
		return tx, err
	}

	tx.Owner = src.Owner
	tx.Amount = src.Amount
	tx.Type = TX_TYPE_APPROVE

	return tx, nil
}

// 从entity中获取amount&orderHash
func CancelView(src *types.OrderCancelledEvent) (TransactionView, error) {
	var tx TransactionView

	tx.Symbol = SYMBOL_EXP
	if err := tx.fullFilled(src.TxInfo); err != nil {
		return tx, err
	}

	tx.Owner = src.From
	tx.Amount = src.AmountCancelled
	tx.Type = TX_TYPE_CANCEL_ORDER

	return tx, nil
}

func CutoffView(src *types.CutoffEvent) (TransactionView, error) {
	var tx TransactionView

	if err := tx.fullFilled(src.TxInfo); err != nil {
		return tx, err
	}
	tx.Symbol = SYMBOL_EXP
	tx.Owner = src.Owner
	tx.Amount = src.Cutoff
	tx.Type = TX_TYPE_CUTOFF

	return tx, nil
}

// 从entity中获取token1,token2
func CutoffPairView(src *types.CutoffPairEvent) (TransactionView, error) {
	var tx TransactionView

	if err := tx.fullFilled(src.TxInfo); err != nil {
		return tx, err
	}

	tx.Symbol = SYMBOL_EXP
	tx.Amount = src.Cutoff
	tx.Owner = src.Owner
	tx.Type = TX_TYPE_CUTOFF_PAIR

	return tx, nil
}

func WexpDepositView(src *types.WexpDepositEvent) ([]TransactionView, error) {
	var (
		list     []TransactionView
		tx1, tx2 TransactionView
	)

	if err := tx1.fullFilled(src.TxInfo); err != nil {
		return list, err
	}

	tx1.Owner = src.Dst
	tx1.Amount = src.Amount
	tx1.Symbol = SYMBOL_EXP
	tx1.Type = TX_TYPE_CONVERT_OUTCOME

	tx2 = tx1
	tx2.Symbol = SYMBOL_WEXP
	tx2.Type = TX_TYPE_CONVERT_INCOME

	list = append(list, tx1, tx2)
	return list, nil
}

func WexpWithdrawalView(src *types.WexpWithdrawalEvent) ([]TransactionView, error) {
	var (
		list     []TransactionView
		tx1, tx2 TransactionView
	)

	if err := tx1.fullFilled(src.TxInfo); err != nil {
		return list, err
	}

	tx1.Owner = src.Src
	tx1.Amount = src.Amount
	tx1.Symbol = SYMBOL_EXP
	tx1.Type = TX_TYPE_CONVERT_INCOME

	tx2 = tx1
	tx2.Symbol = SYMBOL_WEXP
	tx2.Type = TX_TYPE_CONVERT_OUTCOME

	list = append(list, tx1, tx2)

	return list, nil
}

func TransferView(src *types.TransferEvent) ([]TransactionView, error) {
	var (
		list     []TransactionView
		tx1, tx2 TransactionView
	)

	if tx1.Symbol = util.AddressToAlias(src.Protocol.Hex()); tx1.Symbol == "" {
		return list, fmt.Errorf("transaction manager,transfer view, unsupported symbol")
	}
	if err := tx1.fullFilled(src.TxInfo); err != nil {
		return list, err
	}

	tx1.Amount = src.Amount
	tx1.Owner = src.Sender
	tx1.Type = TX_TYPE_SEND

	tx2 = tx1
	tx2.Owner = src.Receiver
	tx2.Type = TX_TYPE_RECEIVE

	list = append(list, tx1, tx2)
	return list, nil
}

func EthTransferView(src *types.EthTransferEvent) ([]TransactionView, error) {
	var (
		list     []TransactionView
		tx1, tx2 TransactionView
	)

	if err := tx1.fullFilled(src.TxInfo); err != nil {
		return list, err
	}

	tx1.Amount = src.Value
	tx1.Symbol = SYMBOL_EXP

	if src.Value.Cmp(big.NewInt(0)) > 0 {
		tx1.Owner = src.From
		tx1.Type = TX_TYPE_SEND

		tx2 = tx1
		tx2.Owner = src.To
		tx2.Type = TX_TYPE_RECEIVE
	} else {
		tx1.Type = TX_TYPE_UNSUPPORTED_CONTRACT
		tx1.Owner = src.From

		tx2 = tx1
		tx2.Owner = src.To
	}

	list = append(list, tx1, tx2)
	return list, nil
}

// 用户币种最多3个tokenS,tokenB,pex
// 一个fill只有一个owner,我们这里最多存储3条数据
func OrderFilledView(src *types.OrderFilledEvent) ([]TransactionView, error) {
	var (
		list []TransactionView
	)

	symbolS := util.AddressToAlias(src.TokenS.Hex())
	symbolB := util.AddressToAlias(src.TokenB.Hex())

	if symbolS != "" {
		totalAmountS := big.NewInt(0)
		totalAmountS = new(big.Int).Add(totalAmountS, src.AmountS)
		totalAmountS = new(big.Int).Add(totalAmountS, src.SplitS)
		if symbolS == SYMBOL_PEX {
			totalAmountS = new(big.Int).Add(totalAmountS, src.PexFee)
			totalAmountS = new(big.Int).Sub(totalAmountS, src.PexReward)
		}

		var tx TransactionView
		if err := tx.fullFilled(src.TxInfo); err != nil {
			return list, err
		}

		tx.Owner = src.Owner
		tx.Symbol = symbolS
		tx.Type = TX_TYPE_SELL
		tx.Amount = totalAmountS
		list = append(list, tx)
	}

	if symbolB != "" {
		totalAmountB := big.NewInt(0)
		totalAmountB = new(big.Int).Add(totalAmountB, src.AmountB)
		totalAmountB = new(big.Int).Sub(totalAmountB, src.SplitB)
		if symbolB == SYMBOL_PEX {
			totalAmountB = new(big.Int).Add(totalAmountB, src.PexReward)
			totalAmountB = new(big.Int).Sub(totalAmountB, src.PexFee)
		}

		var tx TransactionView
		if err := tx.fullFilled(src.TxInfo); err != nil {
			return list, err
		}
		tx.Owner = src.Owner
		tx.Symbol = symbolB
		tx.Type = TX_TYPE_BUY
		tx.Amount = totalAmountB
		list = append(list, tx)
	}

	// pexReward&pexFee只会有一个大于0
	if symbolS != SYMBOL_PEX && symbolB != SYMBOL_PEX {
		var tx TransactionView
		if err := tx.fullFilled(src.TxInfo); err != nil {
			return list, err
		}
		tx.Owner = src.Owner
		tx.Symbol = SYMBOL_PEX

		if src.PexFee.Cmp(big.NewInt(0)) > 0 {
			tx.Type = TX_TYPE_PEX_FEE
			tx.Amount = src.PexFee
			list = append(list, tx)
		} else if src.PexReward.Cmp(big.NewInt(0)) > 0 {
			tx.Type = TX_TYPE_PEX_REWARD
			tx.Amount = src.PexReward
			list = append(list, tx)
		}
	}

	return list, nil
}

func (tx *TransactionView) fullFilled(src types.TxInfo) error {
	if src.Nonce == nil || src.GasLimit == nil || src.GasPrice == nil {
		return fmt.Errorf("transaction manager, full fill tx view error: nonce/gas/gasPrice cann't be nill")
	}

	tx.TxHash = src.TxHash
	if src.BlockNumber != nil {
		tx.BlockNumber = src.BlockNumber.Int64()
	}
	tx.LogIndex = src.TxLogIndex
	tx.Status = src.Status
	tx.Nonce = src.Nonce
	tx.CreateTime = src.BlockTime
	tx.UpdateTime = src.BlockTime

	return nil
}
