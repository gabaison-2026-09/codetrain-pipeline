### 定義

出力予測（output_prediction）。コード片を実行したときに
**標準出力・コンソールへ表示される値**を 4 択から選ばせる。

### 作問方針

- `code` は必須。指定言語で 12 行以内。必ず出力を伴う（`console.log` / `print` など）。
- 出力は決定的にする。乱数・時刻・実行環境依存の結果は使わない。
- `body` は「次のコードを実行したときの出力として正しいものはどれですか。」を基本形にする。
- 正解は 1 個（`correct_keys` は要素 1）。
- ひっかけは「型強制の誤解」「非同期の実行順序の誤解」「メソッドの返り値の取り違え」
  「浮動小数の丸め」など、実際に出やすい誤答にする。
- 選択肢の表記は出力そのもの（例: `[2, 4, 6]`、`"3"`、`undefined`）に統一する。

### 出力例

{"type":"output_prediction","title":"map の返り値","body":"次のコードを実行したときの出力として正しいものはどれですか。","code":"const a = [1, 2, 3];\nconst b = a.map(x => x * 2);\nconsole.log(b);","code_language":"javascript","choices":[{"key":"a","text":"[1, 2, 3]"},{"key":"b","text":"[2, 4, 6]"},{"key":"c","text":"6"},{"key":"d","text":"undefined"}],"correct_keys":["b"],"explanation":"map は各要素にコールバックを適用した新しい配列を返します。元配列 a は変更されず、返り値 b は [2, 4, 6] になります。c は reduce の挙動、d はコールバックの return 漏れと混同した誤答です。","difficulty":2,"tags":["array","map"]}
