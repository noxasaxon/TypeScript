package graph

// AsyncProducer preserves a resolved standard operation's evaluated operands.
// Fulfillment and source-error facts are validated from Kind/Contract; a source
// annotation does not supply producer provenance.
type AsyncProducer struct {
	Kind      AsyncProducerKind
	Position  Position
	Receiver  *Expression
	Arguments []*Expression
	Contract  AsyncProducerContract
}
type AsyncProducerKind string
type AsyncProducerContract string

// AsyncPlatformKind selects the entry ABI; an empty value is the legacy record
// adapter. Standard input identity still requires the actual adapter/constructor
// provenance before an operation may observe the platform object.
type AsyncPlatformKind string

const PlatformStandard AsyncPlatformKind = "standard"
const TypeRequest TypeKind = "intrinsic-request"
const TypeResponse TypeKind = "intrinsic-response"

const (
	ProducerFileUTF8                AsyncProducerKind     = "file-utf8"
	ProducerBodyJSON                AsyncProducerKind     = "body-json"
	ProducerBodyText                AsyncProducerKind     = "body-text"
	ProducerFileUTF8Node24          AsyncProducerContract = "file-utf8/node24"
	ProducerBodyJSONCollectedNode24 AsyncProducerContract = "body-json/collected-node24"
	ProducerBodyTextCollectedNode24 AsyncProducerContract = "body-text/collected-node24"
)

const ExpressionResponseConstruct ExpressionKind = "response-construct"
const ExpressionRequestField ExpressionKind = "request-field"
