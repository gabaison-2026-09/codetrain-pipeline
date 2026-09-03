### 定義

穴埋め（fill_in_blank）。1 箇所を `____`（半角アンダースコア 4 個）で伏せた
コード片を示し、**空欄に入れると意図どおり動く選択肢**を 4 択から選ばせる。

### 作問方針

- `code` は必須。指定言語で 12 行以内。空欄は必ず 1 箇所だけ `____` にする。
- `body` に「このコードで実現したいこと」を明記し、空欄を埋めたら成立するようにする。
- 空欄を埋めれば実際に実行が通り、意図どおり動く問題にする（実行として成立するプログラム）。
- 選択肢は空欄に入る式・メソッド名・引数など、同じ粒度で揃える。
- 正解は 1 個。ひっかけは「似た名前の別メソッド」「引数の順序違い」「1 つずれた境界」など。

### 出力例

{"type":"fill_in_blank","title":"偶数だけ取り出す","body":"配列 nums から偶数だけを取り出して evens に入れたいです。空欄に入るものはどれですか。","code":"const nums = [1, 2, 3, 4, 5, 6];\nconst evens = nums.____(n => n % 2 === 0);\nconsole.log(evens); // [2, 4, 6]","code_language":"javascript","choices":[{"key":"a","text":"filter"},{"key":"b","text":"map"},{"key":"c","text":"forEach"},{"key":"d","text":"find"}],"correct_keys":["a"],"explanation":"filter は条件を満たす要素だけを集めた新しい配列を返すため、evens は [2, 4, 6] になります。map は同じ長さの配列（真偽値の配列）を返し、forEach は undefined を返し、find は最初の 1 要素だけを返すのでいずれも意図と合いません。","difficulty":1,"tags":["array","filter"]}
