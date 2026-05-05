// Intrinsic ID constants. Numerically match
// Include/internal/pycore_intrinsics.h byte-for-byte; the compiler
// emits these as the oparg of CALL_INTRINSIC_1 / CALL_INTRINSIC_2.
//
// CPython: Include/internal/pycore_intrinsics.h

package intrinsics

// Unary IDs (CALL_INTRINSIC_1).
const (
	Unary1Invalid          = 0
	UnaryPrintID           = 1
	UnaryImportStarID      = 2
	UnaryStopIterErrorID   = 3
	UnaryAsyncGenWrapID    = 4
	UnaryPositiveID        = 5
	UnaryListToTupleID     = 6
	UnaryTypevarID         = 7
	UnaryParamspecID       = 8
	UnaryTypevartupleID    = 9
	UnarySubscriptGenericID = 10
	UnaryTypealiasID       = 11

	MaxUnary = UnaryTypealiasID
)

// Binary IDs (CALL_INTRINSIC_2).
const (
	Binary2Invalid                  = 0
	BinaryPrepReraiseStarID         = 1
	BinaryTypevarWithBoundID        = 2
	BinaryTypevarWithConstraintsID  = 3
	BinarySetFunctionTypeParamsID   = 4
	BinarySetTypeparamDefaultID     = 5

	MaxBinary = BinarySetTypeparamDefaultID
)
