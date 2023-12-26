/*
Copyright 2023 KubeAGI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package retriever

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/chains"
	langchaingoschema "github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/vectorstores"
	"github.com/tmc/langchaingo/vectorstores/chroma"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	apiretriever "github.com/kubeagi/arcadia/api/app-node/retriever/v1alpha1"
	"github.com/kubeagi/arcadia/api/base/v1alpha1"
	"github.com/kubeagi/arcadia/pkg/application/base"
	"github.com/kubeagi/arcadia/pkg/langchainwrap"
)

type Reference struct {
	// Question row
	Question string `json:"question" example:"q: 旷工最小计算单位为多少天？"`
	// Answer row
	Answer string `json:"answer" example:"旷工最小计算单位为 0.5 天。"`
	// vector search score
	Score float32 `json:"score" example:"0.34"`
	// file fullpath
	FilePath string `json:"file_path" example:"dataset/dataset-playground/v1/qa.csv"`
	// line number in the file
	LineNumber int `json:"line_number" example:"7"`
}

type KnowledgeBaseRetriever struct {
	langchaingoschema.Retriever
	base.BaseNode
	DocNullReturn string
}

func NewKnowledgeBaseRetriever(ctx context.Context, baseNode base.BaseNode, cli dynamic.Interface) (*KnowledgeBaseRetriever, error) {
	ns := base.GetAppNamespace(ctx)
	instance := &apiretriever.KnowledgeBaseRetriever{}
	obj, err := cli.Resource(schema.GroupVersionResource{Group: apiretriever.GroupVersion.Group, Version: apiretriever.GroupVersion.Version, Resource: "knowledgebaseretrievers"}).
		Namespace(baseNode.Ref.GetNamespace(ns)).Get(ctx, baseNode.Ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("can't find the retriever in cluster: %w", err)
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), instance)
	if err != nil {
		return nil, err
	}
	knowledgebaseName := instance.Spec.Input.KnowledgeBaseRef.Name

	knowledgebase := &v1alpha1.KnowledgeBase{}
	obj, err = cli.Resource(schema.GroupVersionResource{Group: v1alpha1.GroupVersion.Group, Version: v1alpha1.GroupVersion.Version, Resource: "knowledgebases"}).
		Namespace(baseNode.Ref.GetNamespace(ns)).Get(ctx, knowledgebaseName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), knowledgebase)
	if err != nil {
		return nil, err
	}

	embedderReq := knowledgebase.Spec.Embedder
	vectorStoreReq := knowledgebase.Spec.VectorStore
	if embedderReq == nil || vectorStoreReq == nil {
		return nil, fmt.Errorf("knowledgebase %s: embedder or vectorstore or filegroups is not setting", knowledgebaseName)
	}

	embedder := &v1alpha1.Embedder{}
	obj, err = cli.Resource(schema.GroupVersionResource{Group: v1alpha1.GroupVersion.Group, Version: v1alpha1.GroupVersion.Version, Resource: "embedders"}).
		Namespace(embedderReq.GetNamespace(ns)).Get(ctx, embedderReq.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), embedder)
	if err != nil {
		return nil, err
	}
	em, err := langchainwrap.GetLangchainEmbedder(ctx, embedder, nil, cli)
	if err != nil {
		return nil, err
	}
	vectorStore := &v1alpha1.VectorStore{}
	obj, err = cli.Resource(schema.GroupVersionResource{Group: v1alpha1.GroupVersion.Group, Version: v1alpha1.GroupVersion.Version, Resource: "vectorstores"}).
		Namespace(vectorStoreReq.GetNamespace(ns)).Get(ctx, vectorStoreReq.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), vectorStore)
	if err != nil {
		return nil, err
	}
	switch vectorStore.Spec.Type() { // nolint: gocritic
	case v1alpha1.VectorStoreTypeChroma:
		s, err := chroma.New(
			chroma.WithChromaURL(vectorStore.Spec.Endpoint.URL),
			chroma.WithDistanceFunction(vectorStore.Spec.Chroma.DistanceFunction),
			chroma.WithNameSpace(knowledgebase.VectorStoreCollectionName()),
			chroma.WithEmbedder(em),
		)
		if err != nil {
			return nil, err
		}
		return &KnowledgeBaseRetriever{
			vectorstores.ToRetriever(s, instance.Spec.NumDocuments, vectorstores.WithScoreThreshold(instance.Spec.ScoreThreshold)),
			baseNode,
			instance.Spec.DocNullReturn,
		}, nil
	default:
		return nil, fmt.Errorf("unknown vectorstore type: %s", vectorStore.Spec.Type())
	}
}

func (l *KnowledgeBaseRetriever) Run(ctx context.Context, _ dynamic.Interface, args map[string]any) (map[string]any, error) {
	args["retriever"] = l
	return args, nil
}

// KnowledgeBaseStuffDocuments is similar to chains.StuffDocuments but with new joinDocuments method
type KnowledgeBaseStuffDocuments struct {
	chains.StuffDocuments
	isDocNullReturn bool
	DocNullReturn   string
	callbacks.SimpleHandler
	References []Reference
}

var _ chains.Chain = &KnowledgeBaseStuffDocuments{}
var _ callbacks.Handler = &KnowledgeBaseStuffDocuments{}

func (c *KnowledgeBaseStuffDocuments) joinDocuments(docs []langchaingoschema.Document) string {
	var text string
	docLen := len(docs)
	for k, doc := range docs {
		klog.Infof("KnowledgeBaseRetriever: related doc[%d] raw text: %s, raw score: %f\n", k, doc.PageContent, doc.Score)
		for key, v := range doc.Metadata {
			if str, ok := v.([]byte); ok {
				klog.Infof("KnowledgeBaseRetriever: related doc[%d] metadata[%s]: %s\n", k, key, string(str))
			} else {
				klog.Infof("KnowledgeBaseRetriever: related doc[%d] metadata[%s]: %#v\n", k, key, v)
			}
		}
		answer, _ := doc.Metadata["a"].([]byte)
		text += doc.PageContent
		if len(answer) != 0 {
			text = text + "\na: " + strings.TrimPrefix(strings.TrimSuffix(string(answer), "\""), "\"")
		}
		if k != docLen-1 {
			text += c.Separator
		}
		filepath, _ := doc.Metadata["fileName"].([]byte)
		lineNumber, _ := doc.Metadata["lineNumber"].([]byte)
		line, _ := strconv.Atoi(string(lineNumber))
		c.References = append(c.References, Reference{
			Question:   doc.PageContent,
			Answer:     strings.TrimPrefix(strings.TrimSuffix(string(answer), "\""), "\""),
			Score:      doc.Score,
			FilePath:   strings.TrimPrefix(strings.TrimSuffix(string(filepath), "\""), "\""),
			LineNumber: line,
		})
	}
	klog.Infof("KnowledgeBaseRetriever: finally get related text: %s\n", text)
	if len(text) == 0 {
		c.isDocNullReturn = true
	}
	return text
}

func NewStuffDocuments(llmChain *chains.LLMChain, docNullReturn string) *KnowledgeBaseStuffDocuments {
	return &KnowledgeBaseStuffDocuments{
		StuffDocuments: chains.NewStuffDocuments(llmChain),
		DocNullReturn:  docNullReturn,
		References:     make([]Reference, 0, 5),
	}
}

func (c *KnowledgeBaseStuffDocuments) Call(ctx context.Context, values map[string]any, options ...chains.ChainCallOption) (map[string]any, error) {
	docs, ok := values[c.InputKey].([]langchaingoschema.Document)
	if !ok {
		return nil, fmt.Errorf("%w: %w", chains.ErrInvalidInputValues, chains.ErrInputValuesWrongType)
	}

	inputValues := make(map[string]any)
	for key, value := range values {
		inputValues[key] = value
	}

	inputValues[c.DocumentVariableName] = c.joinDocuments(docs)
	return chains.Call(ctx, c.LLMChain, inputValues, options...)
}

func (c KnowledgeBaseStuffDocuments) GetMemory() langchaingoschema.Memory {
	return c.StuffDocuments.GetMemory()
}

func (c KnowledgeBaseStuffDocuments) GetInputKeys() []string {
	return c.StuffDocuments.GetInputKeys()
}

func (c KnowledgeBaseStuffDocuments) GetOutputKeys() []string {
	return c.StuffDocuments.GetOutputKeys()
}

func (c KnowledgeBaseStuffDocuments) HandleChainEnd(_ context.Context, outputValues map[string]any) {
	if !c.isDocNullReturn {
		return
	}
	klog.Infof("raw llmChain output: %s, but there is no doc return, so set output to %s\n", outputValues[c.LLMChain.OutputKey], c.DocNullReturn)
	outputValues[c.LLMChain.OutputKey] = c.DocNullReturn
}
