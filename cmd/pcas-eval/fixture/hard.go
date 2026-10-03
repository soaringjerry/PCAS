package fixture

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	TimeEntity = "time-entity"
	TimeOnly   = "time-only"
	EntityOnly = "entity-only"
	WrongTime  = "wrong-time"
	Ordinary   = "ordinary"
)

var QueryTypes = []string{TimeEntity, TimeOnly, EntityOnly, WrongTime, Ordinary}

type HardConfig struct {
	SchemaVersion      int    `json:"schema_version"`
	Seed               uint64 `json:"seed"`
	CompetitorsPerCase int    `json:"competitors_per_case"`
	UnrelatedDocuments int    `json:"unrelated_documents"`
	CICasesPerType     int    `json:"ci_cases_per_type"`
}

func LoadHardConfig(dir string) (HardConfig, error) {
	var cfg HardConfig
	raw, err := os.ReadFile(filepath.Join(dir, "hard", "config.json"))
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.SchemaVersion != 1 || cfg.CompetitorsPerCase < 40 || cfg.UnrelatedDocuments < 0 || cfg.CICasesPerType < 1 || cfg.CICasesPerType > 8 || 40*(1+cfg.CompetitorsPerCase)+cfg.UnrelatedDocuments < 2000 {
		return cfg, fmt.Errorf("invalid hard config: require >=2000 documents, >=40 competitors and 1..8 CI cases/type")
	}
	return cfg, nil
}
func LoadTier(dir, tier string, anchor time.Time) (Corpus, Baseline, error) {
	if tier == "basic" {
		c, b, err := Load(dir, anchor)
		return c, b, err
	}
	if tier != "hard" {
		return Corpus{}, Baseline{}, fmt.Errorf("tier must be basic or hard")
	}
	cfg, err := LoadHardConfig(dir)
	if err != nil {
		return Corpus{}, Baseline{}, err
	}
	c, err := GenerateHard(cfg, anchor)
	if err != nil {
		return c, Baseline{}, err
	}
	var b Baseline
	raw, err := os.ReadFile(filepath.Join(dir, "hard", "baseline.json"))
	if err != nil {
		return c, b, err
	}
	if err = json.Unmarshal(raw, &b); err != nil {
		return c, b, err
	}
	if b.MinimumRecall < 0 || b.MinimumRecall > 1 {
		return c, b, fmt.Errorf("invalid hard baseline")
	}
	return c, b, nil
}

// The hash stream is indexed by seed, case and competitor ordinal. Increasing
// noise never changes standard sources, dates, answer facts or query wording.
func hardNumber(seed uint64, key string) uint64 {
	sum := sha256.Sum256([]byte(fmt.Sprintf("pcas-v2/%d/%s", seed, key)))
	return binary.BigEndian.Uint64(sum[:8])
}

var hardPeople = []string{"顾禾", "陶宁", "程芦", "林舟", "温岚", "唐溪", "杜荻", "夏遥", "许澄", "叶砚", "沈葵", "阮岑", "白芷", "梁序", "苏棠", "江蒲", "陆檐", "池榕", "孟澈", "姜芩", "季竹", "宋岫", "韩岚", "秦雁", "楚蘅", "吴篁", "谭榆", "谢筠", "姚珞", "易杉", "方澜", "丁蕙", "辛芙", "赵苒", "曲芷", "郑蘋", "于棣", "段槐", "魏莺", "乔苇"}
var hardPlaces = []string{"榆港", "竹川", "绫州", "苇岛", "榕溪", "芦岑", "枫埠", "松浦", "桐泽", "柳湾", "柏丘", "槐泉", "橡原", "桑桥", "棠镇", "梅城", "桃堤", "杏岭", "梨岗", "柚谷", "蕙滩", "兰渡", "菊塘", "莲洲", "荻坡", "蒲湖", "蓉寨", "芙岩", "蓼甸", "蓁河", "葭台", "萱径", "芸田", "芷岭", "蘅苑", "葵市", "堇坡", "芩湾", "菖岸", "蕨原"}
var hardActions = []string{"修复木框", "核对底片", "绘制河岸图", "检查旧风琴", "封装茶样", "装订诗稿", "校准日晷", "整理漆器"}
var hardTopics = []string{"珐琅胸针", "桦木灯罩", "棉线书签", "银丝扣环", "陶泥杯垫", "鹿皮笔袋", "麻布画套", "软木印章"}

func hardDate(anchor time.Time, year, month, day int) int {
	at := time.Date(year, time.Month(month), day, 0, 0, 0, 0, anchor.Location())
	return int(at.Sub(anchor).Hours() / 24)
}
func hardDoc(id, text string, days int, nature, subject, person, place, role string) Document {
	d := Document{ID: id, Title: "聊天记录", Text: text, ExpressedDays: days, RecordedDays: 0, Connector: "archive", Role: role, Gold: Extraction{Items: []Item{}}}
	if role == "user" {
		acquisition, qualification := "direct", "asserted"
		if subject != "我" {
			acquisition, qualification = "reported", "quoted"
		}
		d.Gold.Items = []Item{{Kind: "memory", Text: text, Nature: nature, Subject: subject, Predicate: "描述", Quote: text, Confidence: 1, Acquisition: acquisition, Qualification: qualification, People: []string{person}, Places: []string{place}, Organizations: []string{}}}
	}
	return d
}
func GenerateHard(cfg HardConfig, anchor time.Time) (Corpus, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	anchor = anchor.In(loc)
	anchor = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, loc)
	c := Corpus{SchemaVersion: 1, Tier: "hard", Persona: "南枝（完全虚构）", Timezone: "Asia/Shanghai"}
	monday := (int(anchor.Weekday()) + 6) % 7
	lastWeek := -monday - 7
	// Freeze all standards before generating any competitors. These templates
	// only encode facts known from their own synthetic source, not retrieval.
	for group, typ := range QueryTypes {
		s := Scenario{ID: "hard-" + typ, Rationale: "以资料自身的表达时间、主体和性质标注；所有问法的标准证据先于干扰生成。"}
		for n := 0; n < 8; n++ {
			index := group*8 + n
			person, place := hardPeople[index], hardPlaces[index]
			key := fmt.Sprintf("hard-%s-%02d", typ, n+1)
			code := fmt.Sprintf("青签%04d", hardNumber(cfg.Seed, key+"/gold")%10000)
			action := hardActions[n]
			days := hardDate(anchor, anchor.Year()-1, 12, 8+n)
			text := fmt.Sprintf("我计划去%s见%s，要做%s；交接编号是%s。", place, person, action, code)
			nature := "plan"
			question := fmt.Sprintf("我去年说去%s见%s准备要干什么来着？", place, person)
			facts := []Fact{{Any: []string{action}}, {Any: []string{code}}}
			why := "唯一在去年由本人明确表达的计划；同名同地的他人计划、事实、AI提案及其他年份是干扰。"
			switch typ {
			case TimeOnly:
				days = lastWeek + n%7
				question = []string{"我上周说了什么计划？", "我上周提过哪些打算来着？", "我上周聊过要做的事有哪些？", "我上周讲过什么计划？", "我上周记过哪些准备？", "我上周告诉过你什么计划？", "我上周说的意向和计划有哪些？", "我上周提到要干什么来着？"}[n]
				why = "上周本人明确计划的完整集合为本组八份资料；其他人的计划、本人事实和其他时间的计划不应排在它们前面。"
			case EntityOnly:
				days = -90 - n
				nature = "decision"
				text = fmt.Sprintf("我决定去%s见%s，展陈安排采用%s装裱；交接编号是%s。", place, person, action, code)
				question = fmt.Sprintf("和%s有关的展陈安排里，我决定采用什么装裱？", person)
				if n%2 == 1 {
					question = fmt.Sprintf("去%s的展陈安排里，我决定采用什么装裱？", place)
				}
				why = "只有人名或地名条件，没有日期；本人明确决定优先于自己的考虑、他人决定及AI建议。"
			case WrongTime:
				days = hardDate(anchor, anchor.Year()-2, 12, 8+n)
				facts = append(facts, Fact{Any: []string{fmt.Sprint(anchor.Year() - 2)}})
				why = "该人/地所有资料均不在去年；标准证据在前年，是本人最新的明确计划，考R8按实体放宽并说明实际年份。"
			case Ordinary:
				days = -60 - n
				nature = "fact"
				text = fmt.Sprintf("我在%s和%s讨论%s：保养口令是%s。", place, person, hardTopics[n], code)
				question = fmt.Sprintf("%s的保养口令是什么？", hardTopics[n])
				facts = []Fact{{Any: []string{code}}}
				why = "问法没有日期、有效人名或地名；仅靠独有的非实体话题词定位，结构化规划应跳过。"
			}
			d := hardDoc(key+"-gold", text, days, nature, "我", person, place, "user")
			d.Connector = "capture"
			d.Title = "随手记"
			d.RecordedDays = days
			s.Documents = append(s.Documents, d)
			s.Queries = append(s.Queries, Query{ID: key, Type: typ, Text: question, Evidence: []Evidence{{Source: d.ID, Item: 0}}, Facts: facts, Rationale: why})
		}
		c.Scenarios = append(c.Scenarios, s)
	}
	for group, typ := range QueryTypes {
		for n := 0; n < 8; n++ {
			index := group*8 + n
			person, place := hardPeople[index], hardPlaces[index]
			q := &c.Scenarios[group].Queries[n]
			for j := 0; j < cfg.CompetitorsPerCase; j++ {
				key := fmt.Sprintf("%s-noise-%04d", q.ID, j)
				number := hardNumber(cfg.Seed, key)
				code := fmt.Sprintf("灰签%05d", number%100000)
				action := hardActions[(n+1+j%7)%8]
				days := hardDate(anchor, anchor.Year()-1, 1+int(number%11), 1+int(number%27))
				nature, subject, role := "plan", "我", "user"
				text := fmt.Sprintf("我计划去%s见%s，要做%s；交接编号是%s。", place, person, action, code)
				switch j % 4 {
				case 0:
					days = hardDate(anchor, anchor.Year()-3-int(number%2), 1+int(number%11), 1+int(number%27))
				case 1:
					subject = person
					text = fmt.Sprintf("我听%s说，他计划去%s，要做%s；交接编号是%s。", person, place, action, code)
				case 2:
					nature = "fact"
					text = fmt.Sprintf("我去%s见过%s，聊的是%s而非个人计划；交接编号是%s。", place, person, action, code)
				case 3:
					role = "assistant"
					text = fmt.Sprintf("AI提案：你计划去%s见%s，要做%s；交接编号是%s。", place, person, action, code)
				}
				if typ == TimeOnly && j%4 != 0 {
					days = lastWeek + int(number%7)
				}
				if typ == WrongTime {
					days = hardDate(anchor, anchor.Year()-3-int(number%2), 1+int(number%11), 1+int(number%27))
				}
				if typ == EntityOnly {
					days = -120 - int(number%300)
					switch j % 4 {
					case 0:
						nature = "intention"
						text = fmt.Sprintf("我考虑去%s见%s，展陈安排可能采用%s装裱，尚未决定；交接编号是%s。", place, person, action, code)
					case 1:
						subject = person
						nature = "decision"
						text = fmt.Sprintf("我听%s说，他决定去%s，展陈安排采用%s装裱；交接编号是%s。", person, place, action, code)
					case 2:
						nature = "fact"
						text = fmt.Sprintf("我在%s见过%s，比较过%s装裱，尚未决定；交接编号是%s。", place, person, action, code)
					case 3:
						text = fmt.Sprintf("AI提案：决定去%s见%s，展陈安排采用%s装裱；交接编号是%s。", place, person, action, code)
					}
				}
				d := hardDoc(key, text, days, nature, subject, person, place, role)
				if typ == EntityOnly && j%4 == 0 {
					d.Gold.Items[0].Qualification = "tentative"
				}
				c.Noise = append(c.Noise, d)
				q.Distractors = append(q.Distractors, d.ID)
			}
		}
	}
	// All time-only questions ask for the same complete, bounded set of eight
	// self plans. Their shared evidence is not hand-selected after measurement.
	var weekEvidence []Evidence
	var weekFacts []Fact
	var weekNoise []string
	for _, q := range c.Scenarios[1].Queries {
		weekEvidence = append(weekEvidence, q.Evidence...)
		weekFacts = append(weekFacts, q.Facts...)
		weekNoise = append(weekNoise, q.Distractors...)
	}
	for n := range c.Scenarios[1].Queries {
		q := &c.Scenarios[1].Queries[n]
		q.Evidence = append([]Evidence(nil), weekEvidence...)
		q.Facts = append([]Fact(nil), weekFacts...)
		q.Distractors = append([]string(nil), weekNoise...)
	}
	for n := 0; n < cfg.UnrelatedDocuments; n++ {
		text := fmt.Sprintf("我记录窗台盆栽的浇水量为%d毫升，笔记编号闲页%04d。", 50+hardNumber(cfg.Seed, fmt.Sprintf("unrelated/%d", n))%150, n)
		c.Noise = append(c.Noise, Document{ID: fmt.Sprintf("hard-unrelated-%04d", n), Title: "随手记", Text: text, ExpressedDays: -5 - n, RecordedDays: -5 - n, Connector: "capture", Role: "user", Gold: Extraction{Items: []Item{{Kind: "memory", Text: text, Nature: "fact", Subject: "我", Predicate: "描述", Quote: text, Confidence: 1, Acquisition: "direct", Qualification: "asserted", People: []string{}, Places: []string{}, Organizations: []string{}}}}})
	}
	return c, c.Validate()
}

// Classify is evaluation metadata, not product planning. Existing basic gold
// is unchanged; its grouping only uses the frozen question and entity names.
func QueryType(c Corpus, q Query) string {
	if q.Type != "" {
		return q.Type
	}
	if q.ID == "yp-q1" || q.ID == "yo-q1" {
		return WrongTime
	}
	timeCondition := strings.Contains(q.Text, "去年") || strings.Contains(q.Text, "上周") || strings.Contains(q.Text, "前年")
	entityCondition := false
	for _, d := range c.Documents() {
		for _, i := range d.Gold.Items {
			for _, names := range [][]string{i.People, i.Places} {
				for _, name := range names {
					if len([]rune(name)) >= 2 && strings.Contains(q.Text, name) {
						entityCondition = true
					}
				}
			}
		}
	}
	if timeCondition && entityCondition {
		return TimeEntity
	}
	if timeCondition {
		return TimeOnly
	}
	if entityCondition {
		return EntityOnly
	}
	return Ordinary
}
func CIQueries(c Corpus, perType int) []Query {
	seen := map[string]int{}
	var out []Query
	for _, q := range c.Queries() {
		typ := QueryType(c, q)
		if seen[typ] < perType {
			out = append(out, q)
			seen[typ]++
		}
	}
	return out
}
