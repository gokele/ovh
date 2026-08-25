package catalog

import (
	"regexp"
	"strings"
)

var modelPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-\d+skl[a-e]\d{2}(-v\d+)?`),
	regexp.MustCompile(`-\d+sk\d+`),
	regexp.MustCompile(`-\d+rise\d*`),
	regexp.MustCompile(`-\d+sys\w*`),
	regexp.MustCompile(`-\d+risegame\d*`),
	regexp.MustCompile(`-\d+risestor`),
	regexp.MustCompile(`-\d+skgame\d*`),
	regexp.MustCompile(`-\d+ska\d*`),
	regexp.MustCompile(`-\d+skstor\d*`),
	regexp.MustCompile(`-\d+sysstor`),
	regexp.MustCompile(`game\d*`),
	regexp.MustCompile(`stor\d*`),
	regexp.MustCompile(`-ks\d+`),
	regexp.MustCompile(`-rise`),
	regexp.MustCompile(`-\d+sysle\d+`),
	regexp.MustCompile(`-\d+skb\d+`),
	regexp.MustCompile(`-\d+skc\d+`),
	regexp.MustCompile(`-\d+sk\d+b`),
	regexp.MustCompile(`-v\d+`),
	regexp.MustCompile(`-[a-z]{3}$`),
}

var (
	reEcc       = regexp.MustCompile(`-(no)?ecc-\d+`)
	reStorSfx   = regexp.MustCompile(`-(sas|sa|ssd|nvme)$`)
	reSpecDigit = regexp.MustCompile(`-\d{4,5}$`)
)

func StandardizeConfig(config string) string {
	if config == "" {
		return ""
	}
	normalized := strings.TrimSpace(strings.ToLower(config))
	for _, p := range modelPatterns {
		normalized = p.ReplaceAllString(normalized, "")
	}
	normalized = reEcc.ReplaceAllString(normalized, "")
	normalized = reStorSfx.ReplaceAllString(normalized, "")
	normalized = reSpecDigit.ReplaceAllString(normalized, "")
	return normalized
}

func FormatMemoryDisplay(memoryCode string) string {
	if m := regexp.MustCompile(`(?i)(\d+)g`).FindStringSubmatch(memoryCode); m != nil {
		return m[1] + "GB RAM"
	}
	return memoryCode
}

func FormatStorageDisplay(storageCode string) string {
	if m := regexp.MustCompile(`(?i)(\d+)x(\d+)(ssd|nvme|hdd)`).FindStringSubmatch(storageCode); m != nil {
		return m[1] + "x " + m[2] + "GB " + strings.ToUpper(m[3])
	}
	return storageCode
}

func FormatConfigDisplay(memoryCode, storageCode string) string {
	mem := "默认内存"
	if memoryCode != "" {
		mem = FormatMemoryDisplay(memoryCode)
	}
	stor := "默认存储"
	if storageCode != "" {
		stor = FormatStorageDisplay(storageCode)
	}
	return mem + " + " + stor
}

func MatchConfig(userMemory, userStorage, ovhMemory, ovhStorage string) bool {
	memoryMatch := true
	if userMemory != "" && ovhMemory != "" {
		memoryMatch = StandardizeConfig(userMemory) == StandardizeConfig(ovhMemory)
	}
	storageMatch := true
	if userStorage != "" && ovhStorage != "" {
		storageMatch = StandardizeConfig(userStorage) == StandardizeConfig(ovhStorage)
	}
	return memoryMatch && storageMatch
}
