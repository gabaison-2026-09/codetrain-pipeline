// Package schema は LLM の構造化出力（JSON）の型と、その JSON Schema、
// および DB ドメイン型（codetrain-core/pkg/domain.Question）への変換を持つ。
//
// DESIGN.md §4 の出力形:
//
//	{ type, title, body, code, code_language,
//	  choices[], correct_index / correct_keys, explanation,
//	  difficulty(1-5), topic_tags[] }
//
// CodeTrain の DB は correct_index ではなく correct_keys（key の配列）を使うため
// （DB_SCHEMA.md §5 question）、LLM にも key ベースで返させる。
package schema

import (
	"encoding/json"
	"fmt"

	"github.com/gabaison-2026-09/codetrain-core/pkg/domain"
)

// QuestionDraft は LLM が返す 1 問分の生データ。
// この時点では未検証で、validate パッケージがチェックする。
type QuestionDraft struct {
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	Body         string          `json:"body"`
	Code         string          `json:"code"`
	CodeLanguage string          `json:"code_language"`
	Choices      []domain.Choice `json:"choices"`
	CorrectKeys  []string        `json:"correct_keys"`
	Explanation  string          `json:"explanation"`
	Difficulty   int             `json:"difficulty"`
	Tags         []string        `json:"tags"`
}

// Parse は LLM の応答本文（JSON 文字列）を QuestionDraft にする。
// コードフェンス（```json ... ```）で包まれている場合は剥がす。
func Parse(raw string) (QuestionDraft, error) {
	trimmed := stripCodeFence(raw)
	var d QuestionDraft
	if err := json.Unmarshal([]byte(trimmed), &d); err != nil {
		return QuestionDraft{}, fmt.Errorf("LLM 応答の JSON パースに失敗: %w", err)
	}
	return d, nil
}

// ToDomain は検証済みの QuestionDraft を、DB へ挿入する domain.Question にする。
// raw_source_id は PoC 方針により LLM 生成ダミー行を指す（domain.LLMGeneratedRawSourceID）。
// skillNodeID が空文字なら未マッピング（question.skill_node_id は NULL 可）。
func (d QuestionDraft) ToDomain(skillNodeID string) domain.Question {
	q := domain.Question{
		Type:         domain.QuestionType(d.Type),
		Status:       domain.QuestionStatusNeedsReview,
		Difficulty:   d.Difficulty,
		Title:        d.Title,
		Body:         d.Body,
		Code:         d.Code,
		CodeLanguage: d.CodeLanguage,
		Choices:      d.Choices,
		CorrectKeys:  d.CorrectKeys,
		Explanation:  d.Explanation,
		Tags:         d.Tags,
	}
	if skillNodeID != "" {
		q.SkillNodeID = &skillNodeID
	}
	return q
}

// JSONSchema は LLM に structured output として渡す JSON Schema（Draft 2020-12 相当）。
// プロンプトの静的プレフィックスに埋め込む（将来のプロンプトキャッシュ対象）。
const JSONSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["type", "title", "body", "choices", "correct_keys", "explanation", "difficulty", "tags"],
  "properties": {
    "type": {"type": "string", "enum": ["code_reading", "output_prediction", "bug_finding", "fill_in_blank", "best_practice"]},
    "title": {"type": "string", "minLength": 1, "maxLength": 80},
    "body": {"type": "string", "minLength": 1},
    "code": {"type": "string"},
    "code_language": {"type": "string"},
    "choices": {
      "type": "array", "minItems": 3, "maxItems": 5,
      "items": {
        "type": "object", "additionalProperties": false,
        "required": ["key", "text"],
        "properties": {
          "key": {"type": "string", "enum": ["a", "b", "c", "d", "e"]},
          "text": {"type": "string", "minLength": 1}
        }
      }
    },
    "correct_keys": {"type": "array", "minItems": 1, "items": {"type": "string", "enum": ["a", "b", "c", "d", "e"]}},
    "explanation": {"type": "string", "minLength": 1},
    "difficulty": {"type": "integer", "minimum": 1, "maximum": 5},
    "tags": {"type": "array", "items": {"type": "string"}}
  }
}`

func stripCodeFence(s string) string {
	t := s
	// 先頭・末尾の空白を落とす
	for len(t) > 0 && (t[0] == ' ' || t[0] == '\n' || t[0] == '\r' || t[0] == '\t') {
		t = t[1:]
	}
	for len(t) > 0 {
		last := t[len(t)-1]
		if last == ' ' || last == '\n' || last == '\r' || last == '\t' {
			t = t[:len(t)-1]
			continue
		}
		break
	}
	if len(t) >= 3 && t[:3] == "```" {
		// 最初の改行まで（```json など）を捨てる
		for i := 0; i < len(t); i++ {
			if t[i] == '\n' {
				t = t[i+1:]
				break
			}
		}
		if idx := lastIndex(t, "```"); idx >= 0 {
			t = t[:idx]
		}
	}
	return t
}

func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
