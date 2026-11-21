package postgres

import (
	"fmt"
	"log/slog"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
)

// ConvertNumericToInt64 converts pgtype.Numeric to int64 without losing precision
func ConvertNumericToInt64(num pgtype.Numeric) (int64, error) {
	if !num.Valid {
		return 0, fmt.Errorf("invalid numeric value provided %v", num)
	}

	// if exponent is negative, divide by 10^(-exp)
	// if exponent is positive, multiply by 10^(exp)
	if num.Exp < 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-num.Exp)), nil)
		result := new(big.Int).Div(num.Int, divisor)

		if !result.IsInt64() {
			return 0, fmt.Errorf("numeric value %v overflows int64 range", num)
		}
		return result.Int64(), nil
	} else {
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(num.Exp)), nil)
		scaledValue := new(big.Int).Mul(num.Int, scale)

		if !scaledValue.IsInt64() {
			return 0, fmt.Errorf("numeric value %v overflows int64 range", num)
		}
		return scaledValue.Int64(), nil
	}
}

// IntToNumeric converts an int64 to pgtype.Numeric
func IntToNumeric(num int64) pgtype.Numeric {
	return pgtype.Numeric{Valid: true, Int: big.NewInt(num)}
}

func StringToUUID(uuidStr string) (pgtype.UUID, error) {
	// Create a new pgtype.UUID
	var uuid pgtype.UUID
	// Parse the string into the UUID
	err := uuid.Scan(uuidStr)
	if err != nil {
		slog.Error("Error parsing UUID: %v\n", "err", err)
		return pgtype.UUID{}, fmt.Errorf("failed to parse UUID: %w", err)
	}
	return uuid, nil
}

func UUIDToString(uuid pgtype.UUID) (string, error) {
	if !uuid.Valid {
		return "", fmt.Errorf("invalid UUID value provided %v", uuid)
	}
	return uuid.String(), nil
}
