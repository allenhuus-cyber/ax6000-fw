package hosts

import "strings"

// LookupVendor 根据 MAC 的 OUI（前 3 字节）猜测设备厂商。
// 内置常见厂商，未命中返回空串。可在固件中用完整 OUI 库替换本表。
func LookupVendor(mac string) string {
	m := strings.ToLower(mac)
	m = strings.NewReplacer(":", "", "-", "", ".", "").Replace(m)
	if len(m) < 6 {
		return ""
	}
	return ouiTable[m[:6]]
}

// ouiTable 覆盖国内常见手机/家电/物联网厂商。
var ouiTable = map[string]string{
	// Apple
	"f0d4e2": "Apple", "3c15c2": "Apple", "a4c361": "Apple", "f82793": "Apple",
	"5cf5da": "Apple", "b8e856": "Apple", "8c8590": "Apple", "14c91f": "Apple",
	"f0f6a1": "Apple", "d89695": "Apple", "60f81d": "Apple", "7c6df8": "Apple",
	// 华为 / 荣耀
	"0c96bf": "HUAWEI", "4c54bf": "HUAWEI", "8c7067": "HUAWEI", "ac4e91": "HUAWEI",
	"d46aa8": "HUAWEI", "f4b520": "HUAWEI", "20c5e3": "HUAWEI", "a8b8c9": "HUAWEI",
	"dc09b2": "HUAWEI", "0cb3fd": "HUAWEI",
	// 小米 / Redmi
	"4cf1b8": "Xiaomi", "64cc2e": "Xiaomi", "78b6dd": "Xiaomi", "8cdee5": "Xiaomi",
	"9cb62f": "Xiaomi", "a0c589": "Xiaomi", "b0e235": "Xiaomi", "c4f3ce": "Xiaomi",
	"dcfb02": "Xiaomi", "f0b429": "Xiaomi", "7c2ebd": "Xiaomi", "18e2bb": "Xiaomi",
	"0cf3ee": "Xiaomi", "28ffa2": "Xiaomi",
	// OPPO / vivo / 一加
	"c8f9f6": "OPPO", "e077b7": "OPPO", "a45e60": "OPPO", "8cf5a5": "OPPO",
	"e0b9a5": "vivo", "cc2d1b": "vivo", "9cdfaf": "vivo", "7043cb": "vivo",
	"94e1f5": "OnePlus", "cc3a61": "OnePlus",
	// 三星
	"e4b0d1": "Samsung", "a0f3c1": "Samsung", "5c497d": "Samsung", "b0377d": "Samsung",
	"8c7712": "Samsung", "c4d6af": "Samsung",
	// 电脑 / 网卡
	"8c1645": "Intel", "a4b1c1": "Intel", "3c970e": "Intel", "68ecc8": "Intel",
	"00d861": "ASUSTek", "1c872c": "ASUSTek", "50465d": "ASUSTek", "b827eb": "Raspberry Pi",
	"dca632": "Raspberry Pi", "e45f01": "Raspberry Pi",
	"bcec5d": "TP-Link", "50c7bf": "TP-Link", "98ded0": "TP-Link", "ec26ca": "TP-Link",
	"c025e9": "TP-Link",
	// 物联网 / 智能硬件
	"24a160": "Espressif", "30aea4": "Espressif", "a020a6": "Espressif", "84cca8": "Espressif",
	"dca4ca": "Google", "f4f5d8": "Google", "54eff8": "Google Nest",
	"44af28": "Amazon", "f0d2f1": "Amazon", "681ab2": "Amazon",
	"18b430": "Nintendo", "7cf05b": "Sony", "001a11": "Sony",
	"0016ea": "Nintendo", "dc68eb": "Microsoft", "7c1e52": "Microsoft",
	"98af65": "Tuya", "10d561": "Tuya", "5888b8": "Tuya",
	"2cfda1": "BroadLink", "b4430d": "BroadLink",
}
