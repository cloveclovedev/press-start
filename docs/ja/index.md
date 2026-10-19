# press-start の文書

press-start は、1人のための朝のアラームである。Google カレンダーの最初の予定とアラーム設定から起床時刻を決め、家のサーバーにつないだスピーカーで鳴らし、これからの予定と起きる理由を日本語で読み上げる。Withings Sleep がベッドを出たと知らせるまで、音量を上げて繰り返す。

このページは、`docs/ja/` にあるすべての文書の入口である。

## 初めて読むとき

次の順に読むと、全体がつかめる。

1. [要求仕様書](requirements/overview.md): 何を解決したいか、どこまでやるか、どの順に作るか
2. [用語集](requirements/glossary.md): 文書の中の語の意味
3. カテゴリごとの要件: [起床時刻とアラーム音](requirements/alarm/index.md)、[予定の読み上げ](requirements/brief/index.md)、[ベッド判定](requirements/bed/index.md)、[家のサーバーで動かす](requirements/deploy/index.md)
4. [技術要素](requirements/technology.md): 何で作るか、なぜそれを選んだか

## 文書の一覧

| 文書 | 内容 |
|---|---|
| [要求と要件](requirements/index.md) | 要求仕様書、要件、技術要素、用語集、バックログの入口 |
| [ソースコード全体構成](architecture/overview.md) | パッケージと、その役割と設計。コードを書き始めてから埋める |
