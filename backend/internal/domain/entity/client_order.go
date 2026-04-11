package entity

// ClientOrderStatus はクライアント注文 (冪等性キー単位の注文ライフサイクル) の状態。
//
// pending → confirmed → completed が正常系。pending/submitted は楽天側の真実が
// 不明な状態を表し、reconcile ジョブによって reconciled-* に確定する。
type ClientOrderStatus string

const (
	// ClientOrderStatusPending は Backend が DB に記録した直後 (楽天 HTTP 未開始)。
	ClientOrderStatusPending ClientOrderStatus = "pending"
	// ClientOrderStatusSubmitted は楽天 HTTP 送信を試みたが応答パース失敗等で結果が不明な状態。
	ClientOrderStatusSubmitted ClientOrderStatus = "submitted"
	// ClientOrderStatusConfirmed は応答パースに成功し orderId を取得済み。
	ClientOrderStatusConfirmed ClientOrderStatus = "confirmed"
	// ClientOrderStatusCompleted は約定処理まで Backend ドメインに反映済み。
	ClientOrderStatusCompleted ClientOrderStatus = "completed"
	// ClientOrderStatusFailed は楽天が明示的に拒否した、または送信前に失敗した。
	ClientOrderStatusFailed ClientOrderStatus = "failed"
	// ClientOrderStatusReconciledConfirmed は reconcile が楽天 GetOrders と突合して受理を確定。
	ClientOrderStatusReconciledConfirmed ClientOrderStatus = "reconciled-confirmed"
	// ClientOrderStatusReconciledNotFound は reconcile が TTL 経過後に対応注文を発見できず確定。
	ClientOrderStatusReconciledNotFound ClientOrderStatus = "reconciled-not-found"
	// ClientOrderStatusReconciledAmbiguous は候補が複数ヒットし自動判定不能。
	ClientOrderStatusReconciledAmbiguous ClientOrderStatus = "reconciled-ambiguous"
	// ClientOrderStatusReconciledTimeout は外部 TTL を超過し reconcile でも解決できなかった最終状態。
	ClientOrderStatusReconciledTimeout ClientOrderStatus = "reconciled-timeout"
)

// ClientOrderIntent はクライアント注文の意図。
type ClientOrderIntent string

const (
	ClientOrderIntentOpen   ClientOrderIntent = "open"
	ClientOrderIntentClose  ClientOrderIntent = "close"
	ClientOrderIntentCancel ClientOrderIntent = "cancel"
)
