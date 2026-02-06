package journal

import (
	"database/sql"
	"fmt"
	"time"
)

// GetTrade returns a single trade record by ID.
func (j *SQLite) GetTrade(tradeID string) (TradeRecord, error) {
	var rec TradeRecord

	row := j.db.QueryRow(`
		SELECT trade_id, instrument, units, entry_price, exit_price, open_time, close_time, realized_pl, reason
		FROM trades
		WHERE trade_id = ?`, tradeID)

	err := row.Scan(
		&rec.TradeID,
		&rec.Instrument,
		&rec.Units,
		&rec.EntryPrice,
		&rec.ExitPrice,
		&rec.OpenTime,
		&rec.CloseTime,
		&rec.RealizedPL,
		&rec.Reason,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return TradeRecord{}, fmt.Errorf("trade %q not found", tradeID)
		}
		return TradeRecord{}, err
	}
	return rec, nil
}

// ListTradesClosedBetween returns trades whose close_time is within [start, end).
func (j *SQLite) ListTradesClosedBetween(start, end time.Time) ([]TradeRecord, error) {
	rows, err := j.db.Query(`
		SELECT trade_id, instrument, units, entry_price, exit_price, open_time, close_time, realized_pl, reason
		FROM trades
		WHERE close_time >= ? AND close_time < ?
		ORDER BY close_time ASC`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TradeRecord
	for rows.Next() {
		var rec TradeRecord
		if err := rows.Scan(
			&rec.TradeID,
			&rec.Instrument,
			&rec.Units,
			&rec.EntryPrice,
			&rec.ExitPrice,
			&rec.OpenTime,
			&rec.CloseTime,
			&rec.RealizedPL,
			&rec.Reason,
		); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}

	/* Can add this
	GrossProfit = sum(realized_pl where >0)
	GrossLoss = abs(sum(realized_pl where <0))
	ProfitFactor = GrossProfit / GrossLoss (if GrossLoss > 0)
	*/

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (j *SQLite) ListEquityBetween(start, end time.Time) ([]TradeRecord, error) {

	rows, err := j.db.Query(`
		SELECT time, balance, equity, margin_used, free_margin, margin_level
		FROM equity
		WHERE time >= ? AND time < ?
		ORDER BY time ASC;`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TradeRecord
	for rows.Next() {
		var rec TradeRecord
		if err := rows.Scan(
			&rec.TradeID,
			&rec.Instrument,
			&rec.Units,
			&rec.EntryPrice,
			&rec.ExitPrice,
			&rec.OpenTime,
			&rec.CloseTime,
			&rec.RealizedPL,
			&rec.Reason,
		); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
