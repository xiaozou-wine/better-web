// Package geo 负责把代理出口地映射成自洽的时区与语言。
//
// IP 出口地、时区、语言三者不一致是最容易被检测到的矛盾：代理出口在洛杉矶
// 而浏览器报 Asia/Shanghai 基本等同于自报身份。因此这三项必须由出口 IP
// 自动推导，而不是让用户手填。
package geo

import "better-web/internal/model"

// countryDefault 是各国家/地区的默认时区与语言。
// 跨多时区的国家取人口最集中的时区。
var countryDefault = map[string]struct {
	timezone string
	locale   string
}{
	"US": {"America/New_York", "en-US"},
	"CA": {"America/Toronto", "en-CA"},
	"GB": {"Europe/London", "en-GB"},
	"IE": {"Europe/Dublin", "en-IE"},
	"DE": {"Europe/Berlin", "de-DE"},
	"FR": {"Europe/Paris", "fr-FR"},
	"NL": {"Europe/Amsterdam", "nl-NL"},
	"ES": {"Europe/Madrid", "es-ES"},
	"IT": {"Europe/Rome", "it-IT"},
	"PL": {"Europe/Warsaw", "pl-PL"},
	"SE": {"Europe/Stockholm", "sv-SE"},
	"CH": {"Europe/Zurich", "de-CH"},
	"RU": {"Europe/Moscow", "ru-RU"},
	"TR": {"Europe/Istanbul", "tr-TR"},
	"BR": {"America/Sao_Paulo", "pt-BR"},
	"MX": {"America/Mexico_City", "es-MX"},
	"AR": {"America/Argentina/Buenos_Aires", "es-AR"},
	"JP": {"Asia/Tokyo", "ja-JP"},
	"KR": {"Asia/Seoul", "ko-KR"},
	"CN": {"Asia/Shanghai", "zh-CN"},
	"HK": {"Asia/Hong_Kong", "zh-HK"},
	"TW": {"Asia/Taipei", "zh-TW"},
	"SG": {"Asia/Singapore", "en-SG"},
	"IN": {"Asia/Kolkata", "en-IN"},
	"ID": {"Asia/Jakarta", "id-ID"},
	"TH": {"Asia/Bangkok", "th-TH"},
	"VN": {"Asia/Ho_Chi_Minh", "vi-VN"},
	"MY": {"Asia/Kuala_Lumpur", "ms-MY"},
	"PH": {"Asia/Manila", "en-PH"},
	"AE": {"Asia/Dubai", "ar-AE"},
	"SA": {"Asia/Riyadh", "ar-SA"},
	"IL": {"Asia/Jerusalem", "he-IL"},
	"AU": {"Australia/Sydney", "en-AU"},
	"NZ": {"Pacific/Auckland", "en-NZ"},
	"ZA": {"Africa/Johannesburg", "en-ZA"},
	"NG": {"Africa/Lagos", "en-NG"},
	"EG": {"Africa/Cairo", "ar-EG"},
}

// usTimezoneByRegion 覆盖美国各州。美国跨 6 个时区，只靠国家码会大量出错，
// 而美国又是代理最常见的出口地，值得单独处理。
var usTimezoneByRegion = map[string]string{
	"CA": "America/Los_Angeles", "WA": "America/Los_Angeles", "OR": "America/Los_Angeles",
	"NV": "America/Los_Angeles",
	"AZ": "America/Phoenix",
	"CO": "America/Denver", "UT": "America/Denver", "NM": "America/Denver",
	"MT": "America/Denver", "WY": "America/Denver", "ID": "America/Denver",
	"TX": "America/Chicago", "IL": "America/Chicago", "MN": "America/Chicago",
	"WI": "America/Chicago", "MO": "America/Chicago", "IA": "America/Chicago",
	"LA": "America/Chicago", "AR": "America/Chicago", "OK": "America/Chicago",
	"KS": "America/Chicago", "NE": "America/Chicago", "AL": "America/Chicago",
	"MS": "America/Chicago", "TN": "America/Chicago", "SD": "America/Chicago",
	"ND": "America/Chicago",
	"NY": "America/New_York", "NJ": "America/New_York", "PA": "America/New_York",
	"MA": "America/New_York", "FL": "America/New_York", "GA": "America/New_York",
	"VA": "America/New_York", "NC": "America/New_York", "SC": "America/New_York",
	"OH": "America/New_York", "MI": "America/New_York", "IN": "America/New_York",
	"MD": "America/New_York", "DC": "America/New_York", "CT": "America/New_York",
	"ME": "America/New_York", "NH": "America/New_York", "VT": "America/New_York",
	"RI": "America/New_York", "DE": "America/New_York", "WV": "America/New_York",
	"KY": "America/New_York",
	"AK": "America/Anchorage",
	"HI": "Pacific/Honolulu",
}

// fallback 是无法识别国家码时的兜底。选美国东部因为它是最大的匿名集，
// 落到一个罕见时区反而更可疑。
var fallback = model.Geo{
	CountryCode: "US",
	Timezone:    "America/New_York",
	Locale:      "en-US",
}

// Resolve 由国家码与地区码推导出自洽的时区与语言。
// region 可为空；对美国出口强烈建议提供，否则时区会落到东部默认值。
func Resolve(countryCode, region string) model.Geo {
	if countryCode == "" {
		return fallback
	}
	def, ok := countryDefault[countryCode]
	if !ok {
		g := fallback
		// 保留真实国家码，便于上层记录与排查，但时区语言走兜底。
		g.CountryCode = countryCode
		return g
	}

	tz := def.timezone
	if countryCode == "US" && region != "" {
		if t, ok := usTimezoneByRegion[region]; ok {
			tz = t
		}
	}
	return model.Geo{CountryCode: countryCode, Timezone: tz, Locale: def.locale}
}

// Fallback 返回兜底地理信息的副本。
func Fallback() model.Geo { return fallback }
