// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package attributesprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/model/pdata"
	conventions "go.opentelemetry.io/collector/model/semconv/v1.6.1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/attraction"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/processor/filterconfig"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/processor/filterset"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/testdata"
)

// Common structure for all the Tests
type testCase struct {
	name               string
	serviceName        string
	inputAttributes    map[string]pdata.AttributeValue
	expectedAttributes map[string]pdata.AttributeValue
}

// runIndividualTestCase is the common logic of passing trace data through a configured attributes processor.
func runIndividualTestCase(t *testing.T, tt testCase, tp component.TracesProcessor) {
	t.Run(tt.name, func(t *testing.T) {
		td := generateTraceData(tt.serviceName, tt.name, tt.inputAttributes)
		assert.NoError(t, tp.ConsumeTraces(context.Background(), td))
		// Ensure that the modified `td` has the attributes sorted:
		sortAttributes(td)
		require.Equal(t, generateTraceData(tt.serviceName, tt.name, tt.expectedAttributes), td)
	})
}

func generateTraceData(serviceName, spanName string, attrs map[string]pdata.AttributeValue) pdata.Traces {
	td := pdata.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	if serviceName != "" {
		rs.Resource().Attributes().UpsertString(conventions.AttributeServiceName, serviceName)
	}
	span := rs.InstrumentationLibrarySpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName(spanName)
	pdata.NewAttributeMapFromMap(attrs).CopyTo(span.Attributes())
	span.Attributes().Sort()
	return td
}

func sortAttributes(td pdata.Traces) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		rs.Resource().Attributes().Sort()
		ilss := rs.InstrumentationLibrarySpans()
		for j := 0; j < ilss.Len(); j++ {
			spans := ilss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				spans.At(k).Attributes().Sort()
			}
		}
	}
}

// TestSpanProcessor_Values tests all possible value types.
func TestSpanProcessor_NilEmptyData(t *testing.T) {
	type nilEmptyTestCase struct {
		name   string
		input  pdata.Traces
		output pdata.Traces
	}
	// TODO: Add test for "nil" Span/Attributes. This needs support from data slices to allow to construct that.
	testCases := []nilEmptyTestCase{
		{
			name:   "empty",
			input:  pdata.NewTraces(),
			output: pdata.NewTraces(),
		},
		{
			name:   "one-empty-resource-spans",
			input:  testdata.GenerateTracesOneEmptyResourceSpans(),
			output: testdata.GenerateTracesOneEmptyResourceSpans(),
		},
		{
			name:   "no-libraries",
			input:  testdata.GenerateTracesNoLibraries(),
			output: testdata.GenerateTracesNoLibraries(),
		},
		{
			name:   "one-empty-instrumentation-library",
			input:  testdata.GenerateTracesOneEmptyInstrumentationLibrary(),
			output: testdata.GenerateTracesOneEmptyInstrumentationLibrary(),
		},
	}
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Settings.Actions = []attraction.ActionKeyValue{
		{Key: "attribute1", Action: attraction.INSERT, Value: 123},
		{Key: "attribute1", Action: attraction.DELETE},
	}

	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), oCfg, consumertest.NewNop())
	require.Nil(t, err)
	require.NotNil(t, tp)
	for i := range testCases {
		tt := testCases[i]
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, tp.ConsumeTraces(context.Background(), tt.input))
			assert.EqualValues(t, tt.output, tt.input)
		})
	}
}

func TestAttributes_FilterSpans(t *testing.T) {
	testCases := []testCase{
		{
			name:            "apply processor",
			serviceName:     "svcB",
			inputAttributes: map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1": pdata.NewAttributeValueInt(123),
			},
		},
		{
			name:        "apply processor with different value for exclude property",
			serviceName: "svcB",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(false),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1":     pdata.NewAttributeValueInt(123),
				"NoModification": pdata.NewAttributeValueBool(false),
			},
		},
		{
			name:               "incorrect name for include property",
			serviceName:        "noname",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
		{
			name:        "attribute match for exclude property",
			serviceName: "svcB",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(true),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(true),
			},
		},
	}

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Actions = []attraction.ActionKeyValue{
		{Key: "attribute1", Action: attraction.INSERT, Value: 123},
	}
	oCfg.Include = &filterconfig.MatchProperties{
		Services: []string{"svcA", "svcB.*"},
		Config:   *createConfig(filterset.Regexp),
	}
	oCfg.Exclude = &filterconfig.MatchProperties{
		Attributes: []filterconfig.Attribute{
			{Key: "NoModification", Value: true},
		},
		Config: *createConfig(filterset.Strict),
	}
	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), cfg, consumertest.NewNop())
	require.Nil(t, err)
	require.NotNil(t, tp)

	for _, tt := range testCases {
		runIndividualTestCase(t, tt, tp)
	}
}

func TestAttributes_FilterSpansByNameStrict(t *testing.T) {
	testCases := []testCase{
		{
			name:            "apply",
			serviceName:     "svcB",
			inputAttributes: map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1": pdata.NewAttributeValueInt(123),
			},
		},
		{
			name:        "apply",
			serviceName: "svcB",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(false),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1":     pdata.NewAttributeValueInt(123),
				"NoModification": pdata.NewAttributeValueBool(false),
			},
		},
		{
			name:               "incorrect_span_name",
			serviceName:        "svcB",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
		{
			name:               "dont_apply",
			serviceName:        "svcB",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
		{
			name:        "incorrect_span_name_with_attr",
			serviceName: "svcB",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(true),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(true),
			},
		},
	}

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Actions = []attraction.ActionKeyValue{
		{Key: "attribute1", Action: attraction.INSERT, Value: 123},
	}
	oCfg.Include = &filterconfig.MatchProperties{
		SpanNames: []string{"apply", "dont_apply"},
		Config:    *createConfig(filterset.Strict),
	}
	oCfg.Exclude = &filterconfig.MatchProperties{
		SpanNames: []string{"dont_apply"},
		Config:    *createConfig(filterset.Strict),
	}
	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), cfg, consumertest.NewNop())
	require.Nil(t, err)
	require.NotNil(t, tp)

	for _, tt := range testCases {
		runIndividualTestCase(t, tt, tp)
	}
}

func TestAttributes_FilterSpansByNameRegexp(t *testing.T) {
	testCases := []testCase{
		{
			name:            "apply_to_span_with_no_attrs",
			serviceName:     "svcB",
			inputAttributes: map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1": pdata.NewAttributeValueInt(123),
			},
		},
		{
			name:        "apply_to_span_with_attr",
			serviceName: "svcB",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(false),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1":     pdata.NewAttributeValueInt(123),
				"NoModification": pdata.NewAttributeValueBool(false),
			},
		},
		{
			name:               "incorrect_span_name",
			serviceName:        "svcB",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
		{
			name:               "apply_dont_apply",
			serviceName:        "svcB",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
		{
			name:        "incorrect_span_name_with_attr",
			serviceName: "svcB",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(true),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(true),
			},
		},
	}

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Actions = []attraction.ActionKeyValue{
		{Key: "attribute1", Action: attraction.INSERT, Value: 123},
	}
	oCfg.Include = &filterconfig.MatchProperties{
		SpanNames: []string{"^apply.*"},
		Config:    *createConfig(filterset.Regexp),
	}
	oCfg.Exclude = &filterconfig.MatchProperties{
		SpanNames: []string{".*dont_apply$"},
		Config:    *createConfig(filterset.Regexp),
	}
	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), cfg, consumertest.NewNop())
	require.Nil(t, err)
	require.NotNil(t, tp)

	for _, tt := range testCases {
		runIndividualTestCase(t, tt, tp)
	}
}

func TestAttributes_Hash(t *testing.T) {
	testCases := []testCase{
		{
			name: "String",
			inputAttributes: map[string]pdata.AttributeValue{
				"user.email": pdata.NewAttributeValueString("john.doe@example.com"),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"user.email": pdata.NewAttributeValueString("73ec53c4ba1747d485ae2a0d7bfafa6cda80a5a9"),
			},
		},
		{
			name: "Int",
			inputAttributes: map[string]pdata.AttributeValue{
				"user.id": pdata.NewAttributeValueInt(10),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"user.id": pdata.NewAttributeValueString("71aa908aff1548c8c6cdecf63545261584738a25"),
			},
		},
		{
			name: "Double",
			inputAttributes: map[string]pdata.AttributeValue{
				"user.balance": pdata.NewAttributeValueDouble(99.1),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"user.balance": pdata.NewAttributeValueString("76429edab4855b03073f9429fd5d10313c28655e"),
			},
		},
		{
			name: "Bool",
			inputAttributes: map[string]pdata.AttributeValue{
				"user.authenticated": pdata.NewAttributeValueBool(true),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"user.authenticated": pdata.NewAttributeValueString("bf8b4530d8d246dd74ac53a13471bba17941dff7"),
			},
		},
	}

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Actions = []attraction.ActionKeyValue{
		{Key: "user.email", Action: attraction.HASH},
		{Key: "user.id", Action: attraction.HASH},
		{Key: "user.balance", Action: attraction.HASH},
		{Key: "user.authenticated", Action: attraction.HASH},
	}

	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), cfg, consumertest.NewNop())
	require.Nil(t, err)
	require.NotNil(t, tp)

	for _, tt := range testCases {
		runIndividualTestCase(t, tt, tp)
	}
}

func TestAttributes_Append(t *testing.T) {
	testCases := []testCase{
		// Ensure no changes to the span as there is no attributes map.
		{
			name:               "AppendNoAttributes",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
		// Ensure no changes to the span as the key does not exist.
		{
			name: "AppendKeyNoExist",
			inputAttributes: map[string]pdata.AttributeValue{
				"boo": pdata.NewAttributeValueString("foo"),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"boo": pdata.NewAttributeValueString("foo"),
			},
		},
		// Ensure the attribute `host.id` is updated.
		{
			name: "AppendAttributes",
			inputAttributes: map[string]pdata.AttributeValue{
				"host.id": pdata.NewAttributeValueString("12345"),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"host.id": pdata.NewAttributeValueString("12345:6379"),
			},
		},
		// Ensure there is no change to attribute since it is not type string.
		{
			name: "AppendAttributes",
			inputAttributes: map[string]pdata.AttributeValue{
				"host.id": pdata.NewAttributeValueInt(1),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"host.id": pdata.NewAttributeValueInt(1),
			},
		},
	}

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Actions = []attraction.ActionKeyValue{
		{Key: "host.id", Action: attraction.APPEND, Value: ":6379"},
	}

	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), cfg, consumertest.NewNop())
	require.Nil(t, err)
	require.NotNil(t, tp)

	for _, tt := range testCases {
		runIndividualTestCase(t, tt, tp)
	}
}

func BenchmarkAttributes_FilterSpansByName(b *testing.B) {
	testCases := []testCase{
		{
			name:            "apply_to_span_with_no_attrs",
			inputAttributes: map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1": pdata.NewAttributeValueInt(123),
			},
		},
		{
			name: "apply_to_span_with_attr",
			inputAttributes: map[string]pdata.AttributeValue{
				"NoModification": pdata.NewAttributeValueBool(false),
			},
			expectedAttributes: map[string]pdata.AttributeValue{
				"attribute1":     pdata.NewAttributeValueInt(123),
				"NoModification": pdata.NewAttributeValueBool(false),
			},
		},
		{
			name:               "dont_apply",
			inputAttributes:    map[string]pdata.AttributeValue{},
			expectedAttributes: map[string]pdata.AttributeValue{},
		},
	}

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	oCfg := cfg.(*Config)
	oCfg.Actions = []attraction.ActionKeyValue{
		{Key: "attribute1", Action: attraction.INSERT, Value: 123},
	}
	oCfg.Include = &filterconfig.MatchProperties{
		SpanNames: []string{"^apply.*"},
	}
	tp, err := factory.CreateTracesProcessor(context.Background(), componenttest.NewNopProcessorCreateSettings(), cfg, consumertest.NewNop())
	require.Nil(b, err)
	require.NotNil(b, tp)

	for _, tt := range testCases {
		td := generateTraceData(tt.serviceName, tt.name, tt.inputAttributes)

		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				assert.NoError(b, tp.ConsumeTraces(context.Background(), td))
			}
		})

		// Ensure that the modified `td` has the attributes sorted:
		sortAttributes(td)
		require.Equal(b, generateTraceData(tt.serviceName, tt.name, tt.expectedAttributes), td)
	}
}

func createConfig(matchType filterset.MatchType) *filterset.Config {
	return &filterset.Config{
		MatchType: matchType,
	}
}
