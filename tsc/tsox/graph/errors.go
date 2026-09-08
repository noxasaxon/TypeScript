package graph

// These categories describe selected native storage, not structural Error
// annotations. Actual intrinsic construction or an admitted host producer must
// establish the source error domain before emission.
const (
	TypeError                 TypeKind       = "source-error"
	TypeThrown                TypeKind       = "source-thrown"
	ExpressionErrorConstruct  ExpressionKind = "error-construct"
	ExpressionErrorInstanceOf ExpressionKind = "error-instanceof"
	ExpressionErrorField      ExpressionKind = "error-field"
)

// ErrorConstruct uses Name for the checker-resolved intrinsic constructor and
// Arguments for every source argument in evaluation order. Type is TypeError.
// ErrorInstanceOf uses Receiver for the original evaluated storage (including a
// catch value of TypeThrown), Name for the resolved intrinsic, and boolean Type.
// ErrorField uses Receiver for original storage, Name="name" or "message", and
// string Type. A narrowed checker type does not replace the receiver's storage.
// Catch binding metadata will be an optional *Parameter on AsyncProgram; its
// Type is TypeThrown, never Error solely because an instanceof use narrows it.
// Existing graphs with no selected source-error component retain String ABI.
