package main

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"gopkg.in/yaml.v2"
	"time"

	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/prometheus/prometheus/pkg/rulefmt"

	. "github.com/smartystreets/goconvey/convey"
)

const(
	myAnno = "nordstrom.net/prometheus2alerts"
)


var (
	c *Controller

	events *ConfigMapEventContainer

	testRules []byte
	testRuleGroup []byte
	testRuleGroups []byte

	configmapDataBlockRules corev1.ConfigMap
	configmapDataBlockRulesGroup corev1.ConfigMap
	configmapDataBlockRulesGroups corev1.ConfigMap
	configmapDataBlockAllThree corev1.ConfigMap
	configmapNoAnnotation corev1.ConfigMap
)

type ConfigMapEventContainer struct {
	Events []ConfigMapEvent
}

type ConfigMapEvent struct {
	CMName    	string
	CMNamespace string
	Eventtype 	string
	Message 	string
	Reason 		string
}


func TestMain(m *testing.M) {

	rsource := rand.NewSource(time.Now().UnixNano())

	testRulesObj := validRulesArray()

	testRuleGroupObj := rulefmt.RuleGroup{
		Name: "TestGroup",
		Rules: testRulesObj,
	}

	testRuleGroupsObj := rulefmt.RuleGroups{
		Groups: []rulefmt.RuleGroup{
			testRuleGroupObj,
		},
	}

	var err error
	testRules, err = yaml.Marshal(testRulesObj)
	if err != nil {
		fmt.Println(err)
	}

	testRuleGroup, err = yaml.Marshal(testRuleGroupObj)
	if err != nil {
		fmt.Println(err)
	}

	testRuleGroups, err = yaml.Marshal(testRuleGroupsObj)
	if err != nil {
		fmt.Println(err)
	}

	configmapDataBlockRules = corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:                       "rules",
			Namespace:                  "default",
			Annotations: map[string]string{myAnno: "true"},
		},
		Data:       nil,
	}

	configmapDataBlockRulesGroup = corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:                       "rules-group",
			Namespace:                  "default",
			Annotations: map[string]string{myAnno: "true"},
		},
		Data:       nil,
	}

	configmapDataBlockRulesGroups = corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:                       "rules-groups",
			Namespace:                  "default",
			Annotations: map[string]string{myAnno: "true"},
		},
		Data:       nil,
	}

	configmapDataBlockAllThree = corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:                       "rules-groups-all-three",
			Namespace:                  "default",
			Annotations: map[string]string{myAnno: "true"},
		},
		Data:       nil,
	}

	configmapNoAnnotation = configmapDataBlockRules
	configmapNoAnnotation.ObjectMeta.Annotations = nil


	configmapDataBlockRules.Data = map[string]string{
		"rules": string(testRules),
	}
	configmapDataBlockRulesGroup.Data = map[string]string{
		"rules": string(testRuleGroup),
	}
	configmapDataBlockRulesGroups.Data = map[string]string{
		"rules": string(testRuleGroup),
	}
	configmapDataBlockAllThree.Data = map[string]string{
		"aaa": string(testRules),
		"bbb": string(testRuleGroup),
		"ccc": string(testRuleGroups),
	}
	configmapNoAnnotation.Data = map[string]string{
		"rules": string(testRules),
	}

	anno := myAnno
	events = &ConfigMapEventContainer{}

	// just what we need
	c = &Controller{
		kubeclientset:              nil,
		configmapsLister:           nil,
		configmapsSynced:           nil,
		workqueue:                  nil,
		recorder:                   nil,
		resourceVersionMap:         make(map[string]string),
		interestingAnnotation:      &anno,
		reloadEndpoint:             nil,
		rulesPath:                  nil,
		randSrc:                    &rsource,
		configmapEventRecorderFunc: events.Add,
	}


	os.Exit(m.Run())
}

func TestExtractRuleGroups(t *testing.T) {
	Convey("Test extractRuleGroups, a properly formatted rule groups", t, func() {

		Convey("Positive test case", func() {
			err, rulegroups := c.extractRuleGroups(string(testRuleGroups))

			So(err, ShouldBeNil)
			So(rulegroups.Groups[0].Name, ShouldEqual, "TestGroup")
			So(rulegroups.Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")

		})

		Convey( "Negative test case", func() {
			err, rulegroups := c.extractRuleGroups(string(testRuleGroup))

			So(err, ShouldNotBeNil)
			So(len(rulegroups.Groups), ShouldEqual, 0)

		})
	})
}

func TestExtractRuleGroupAsRuleGroups(t *testing.T) {
	Convey("Test extractRuleGroupAsRuleGroups, a properly formatted rule group", t, func() {

		Convey("Positive test case", func() {
			err, rulegroups := c.extractRuleGroupAsRuleGroups(string(testRuleGroup))

			So(err, ShouldBeNil)
			So(rulegroups.Groups[0].Name, ShouldEqual, "TestGroup")
			So(rulegroups.Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})
		Convey("Negative test case", func() {
			err, rulegroups := c.extractRuleGroupAsRuleGroups(string(testRuleGroups))

			So(err, ShouldNotBeNil)
			So(len(rulegroups.Groups), ShouldEqual, 0)
		})

	})
}

func TestExtractRulesAsRuleGroups(t *testing.T) {
	Convey("Test extractRuleGroupAsRuleGroups, a properly formatted rule group", t, func() {

		Convey("Positive test case", func() {
			err, rulegroups := c.extractRulesAsRuleGroups("aaaa-bbbb", "cccc", string(testRules))

			So(err, ShouldBeNil)
			So(rulegroups.Groups[0].Name, ShouldEqual, "aaaa-bbbb-cccc")
			So(rulegroups.Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})

		Convey("Negative test case", func() {
			err, rulegroups := c.extractRulesAsRuleGroups("aaaa-bbbb", "cccc", string(testRuleGroup))

			So(err, ShouldNotBeNil)
			So(len(rulegroups.Groups), ShouldEqual, 0)
		})


	})
}

func TestExtractValues(t *testing.T) {
	Convey("Presented with a configmap, the proper values should be extracted regardless of formats, where the format is acceptable", t, func() {
		mrg := c.extractValues(&configmapDataBlockAllThree)
		So(len(mrg.Values), ShouldEqual, 3)
		So(countMultiRuleGroupsRules(mrg), ShouldEqual, 6)
	})

	Convey("Presented with a configmap in the wrong format there should be a warning on the event stream for the configmap", t, func() {
		events.Clear()
		cm := corev1.ConfigMap{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:                       "rules",
				Namespace:                  "default",
				Annotations: map[string]string{myAnno: "true"},
			},
			Data:       nil,
		}

		cm.Data = map[string]string{
			// spacing inside of `` is sacrosanct do not over-format (line 2 should be two spaces indented)
			"test": `- record: job:http_inprogress_requests:sum
  expr: sum(http_inprogress_requests) by (job)`,
					"test2": `failNotARule`,
					"test3": `- record: failNoExpression`,
				}

		mrg := c.extractValues(&cm)
		So(len(mrg.Values), ShouldEqual, 1)
		So(countMultiRuleGroupsRules(mrg), ShouldEqual, 1)
		// 1 key accepted at all
		So(events.CountNormals(), ShouldEqual, 1)
		// 1 rule validation failure (test3 record rule)
		// 1 "key has no rules failure" (test3)
		// 1 key not parsable failure (test2)
		So(events.CountWarnings(), ShouldEqual, 3)
	})
}

func TestIsRuleConfigMap(t *testing.T) {
	Convey("Should correctly detect a properly annotated configmap", t, func() {
		ok := c.isRuleConfigMap(&configmapDataBlockRules)
		ok2 := c.isRuleConfigMap(&configmapNoAnnotation)

		So(ok, ShouldBeTrue)
		So(ok2, ShouldBeFalse)
	})
}


func TestHaveRuleConfigMapsChanged(t *testing.T) {
	Convey("Should detect if configmaps have changed", t, func() {
		cm1 := configmapDataBlockAllThree
		cm2 := configmapDataBlockAllThree

		cm1.ResourceVersion = "0000000001"
		cm2.ResourceVersion = "0000000001"

		list := corev1.ConfigMapList{Items: []corev1.ConfigMap{cm1, cm2}}


		// initial read should be positive
		changed := c.haveConfigMapsChanged(&list)
		So(changed, ShouldBeTrue)

		// run it again, should be false (no changes)
		changed = c.haveConfigMapsChanged(&list)
		So(changed, ShouldBeFalse)

		// tweak then one last time, RV will change on one should be true
		list.Items[1].ResourceVersion = "0000000002"
		changed = c.haveConfigMapsChanged(&list)
		So(changed, ShouldBeTrue)


	})
}

func TestValidateRuleGroups(t *testing.T) {

}

func TestRemoveRules(t *testing.T) {
	Convey("Should properly remove rules from a rule group by index", t, func() {
		rgtest := createRuleGroup()

		c.removeRules(&rgtest, []int{0,2})
		So(len(rgtest.Rules), ShouldEqual, 1)
		So(rgtest.Rules[0].Record, ShouldEqual, "test1")

		rgtest2 := createRuleGroup()
		So(len(rgtest2.Rules), ShouldEqual, 3)

		c.removeRules(&rgtest2, []int{1})

		So(len(rgtest2.Rules), ShouldEqual, 2)
		So(rgtest2.Rules[0].Record, ShouldEqual, "test0")
		So(rgtest2.Rules[1].Record, ShouldEqual, "test2")

		rgtest3 := createRuleGroup()
		So(len(rgtest3.Rules), ShouldEqual, 3)
		c.removeRules(&rgtest3, []int{1,2})
		So(len(rgtest3.Rules), ShouldEqual, 1)
		So(rgtest3.Rules[0].Record, ShouldEqual, "test0")

	})
}

func TestDecomposeMultiRuleGroupIntoRuleGroups(t *testing.T) {
	Convey("Should properly return a single ruleGroups from a MRG", t, func() {
		mrgs := createMultiRuleGroups()

		// prelim confirmation
		So(len(mrgs.Values), ShouldEqual, 2)
		So(countMultiRuleGroupsRules(mrgs), ShouldEqual, 9)

		rgs := c.decomposeMultiRuleGroupIntoRuleGroups(&mrgs)

		So(len(rgs.Groups), ShouldEqual, 3)
		So(countRuleGroupsRules(*rgs), ShouldEqual, 9)
	})
}

func TestSaltRuleGroupNames(t *testing.T) {
	Convey("Group names should be unique after salting", t, func() {
		rgs := createRuleGroups()

		// prelim confirm
		So(len(rgs.Groups), ShouldEqual, 3)
		So(countRuleGroupsRules(rgs), ShouldEqual, 9)
		So(rgs.Groups[0].Name, ShouldEqual, rgs.Groups[1].Name)

		rgs2 := c.saltRuleGroupNames(&rgs)
		So(rgs2.Groups[0].Name, ShouldNotEqual, rgs2.Groups[1].Name)
		So(rgs2.Groups[0].Name, ShouldNotEqual, rgs2.Groups[2].Name)
		So(rgs2.Groups[1].Name, ShouldNotEqual, rgs2.Groups[2].Name)

	})
}

func validRulesArray() []rulefmt.Rule {
	return []rulefmt.Rule {
		rulefmt.Rule{
			Record: "job:http_inprogress_requests:sum",
			Expr: "sum(http_inprogress_requests) by (job)",
		},
		rulefmt.Rule{
			Alert: "HighErrorRate",
			Expr: "job:request_latency_seconds:mean5m{job=\"myjob\"} > 0.5",
			Labels: map[string]string {
				"severity": "Page",
			},
			Annotations: map[string]string {
				"summary": "High request latency",
			},
		},
	}
}

func createRuleGroup() rulefmt.RuleGroup {
	rg := rulefmt.RuleGroup{
		Name:     "Test",
		Interval: 0,
		Rules:    nil,
	}

	rg.Rules = append(rg.Rules, rulefmt.Rule{
		Record:      "test0",
		Expr:        "sum(http_inprogress_requests) by (job, x)",
		For:         0,
		Labels:      nil,
		Annotations: nil,
	})

	rg.Rules = append(rg.Rules, rulefmt.Rule{
		Record:      "test1",
		Expr:        "sum(http_inprogress_requests) by (job, y)",
		For:         0,
		Labels:      nil,
		Annotations: nil,
	})

	rg.Rules = append(rg.Rules, rulefmt.Rule{
		Record:      "test2",
		Expr:        "sum(http_inprogress_requests) by (job, z)",
		For:         0,
		Labels:      nil,
		Annotations: nil,
	})
	return rg
}

func createRuleGroups() rulefmt.RuleGroups {
	rgs := rulefmt.RuleGroups{}
	rgs.Groups = append(rgs.Groups, createRuleGroup())
	rgs.Groups = append(rgs.Groups, createRuleGroup())
	rgs.Groups = append(rgs.Groups, createRuleGroup())

	return rgs
}

// returns a mrg of 2 rulegroups, one which has 2 rg the other has 1
// total of 3 rg and 9 rules
func createMultiRuleGroups() MultiRuleGroups {
	rgs0 := rulefmt.RuleGroups{}

	rgs0.Groups = append(rgs0.Groups, createRuleGroup())
	rgs0.Groups = append(rgs0.Groups, createRuleGroup())

	rgs1 := rulefmt.RuleGroups{}

	rgs1.Groups = append(rgs1.Groups, createRuleGroup())

	mgs := MultiRuleGroups{}
	mgs.Values = []rulefmt.RuleGroups{rgs0, rgs1}

	return mgs
}

func countMultiRuleGroupsRules(mrgs MultiRuleGroups) int {
	count := 0
	for _, rgs := range mrgs.Values {
		for _, rg := range rgs.Groups {
			count += len(rg.Rules)
		}
	}
	return count
}

func countRuleGroupsRules(rgs rulefmt.RuleGroups) int {
	count := 0
	for _, rg := range rgs.Groups {
		count += len(rg.Rules)
	}
	return count
}

func (ce *ConfigMapEventContainer) Clear() {
	ce.Events = make([]ConfigMapEvent,0)
}

func (ce *ConfigMapEventContainer) Add(cm *corev1.ConfigMap, eventtype, reason, msg string) {
	ce.Events = append(ce.Events, ConfigMapEvent{ cm.Name, cm.Namespace, eventtype, msg, reason})
}

func (ce *ConfigMapEventContainer) CountWarnings() int {
	count := 0
	for _, v := range ce.Events {
		if v.Eventtype == corev1.EventTypeWarning {
			count++
		}
	}
	return count
}

func (ce *ConfigMapEventContainer) CountNormals() int {
	count := 0
	for _, v := range ce.Events {
		if v.Eventtype == corev1.EventTypeNormal {
			count++
		}
	}
	return count
}
