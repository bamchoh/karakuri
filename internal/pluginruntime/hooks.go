package pluginruntime

// DataChangeHook はデータ変更時に呼ばれるコールバック型
// プラグインサーバーが SubscribeChanges ストリームで変更通知を送るために使用する。
// isBit=true の場合は bitValues を、isBit=false の場合は values を参照する。
type DataChangeHook func(area string, address uint32, values []uint16, isBit bool, bitValues []bool)
