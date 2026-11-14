---
title: 수집 정의 형식 — 내 엑셀과 CSV를 소스 정의와 매핑 정의로 설명하는 법
kind: guide
status: active
follows:
  - docs/design-docs/ingestion.md
covers:
  - backend/internal/ingest/definition.go
  - backend/internal/ingest/mapping.go
  - backend/internal/ingest/sheetopts.go
  - backend/internal/ingest/refine.go
  - backend/internal/ingest/report.go
  - backend/internal/ingest/workbook.go
  - backend/cmd/praxium/ingestout.go
---
# 수집 정의 형식

내 파일(엑셀, CSV, JSON Lines)로 KG를 만들려면 JSON 파일 두 개를 쓴다. 이 문서는 두 파일의 모든 필드, 실행 결과로 나오는 문제 코드, 처음부터 끝까지 따라 하는 예를 적는다. 적힌 내용은 이 버전의 실행 파일로 직접 돌려 확인했다. 설계의 이유는 [현실 데이터 수집 설계](../design-docs/ingestion.md)에 있다.

## 1. 전체 흐름

파일은 **소스 정의**(`source.json`)가 읽는 방법을 정하고, **매핑 정의**(`mapping.json`)가 읽은 행으로 만들 객체와 링크를 정한다. 소스 정의는 파일마다 데이터셋(행과 컬럼으로 본 표)을 선언한다. 매핑 정의는 데이터셋을 정제하고(`refinements`), 데이터셋마다 객체 타입 하나로 잇고(`feeds`), 객체 사이의 링크를 만든다(`links`). 마지막에 `praxium ingest run`이 두 정의와 파일을 받아 KG를 만든다. 파일은 읽기만 한다. 예제는 `praxium ingest init -out ex`가 쓴다. 이 문서의 사본(`ingest-definitions.md`)도 같은 폴더에 쓴다.

```
praxium ingest inspect -input 파일          # 파일에 무엇이 있는지 본다(쓰기 전)
praxium ingest inspect -input 파일 -emit-source   # 소스 정의 초안을 표준 출력으로 낸다(2.6)
praxium ingest run -data-dir data -source source.json -mapping mapping.json -input 데이터셋=파일 ...
praxium local -data-dir data                # 결과를 웹 UI와 API로 본다
```

정의 파일에 모르는 필드를 쓰면 `unknown field "…"`로 거절한다(철자 오류를 조용히 넘기지 않는다). 거절할 때 그 자리에서 허용되는 필드를 함께 알려 준다.

## 2. 소스 정의 (source.json)

### 2.1 최상위

| 필드 | 필수 | 기본값 | 뜻 |
|---|---|---|---|
| `id` | 예 | | 소스의 이름. 데이터가 어디서 왔는지 보여 줄 때 쓴다 |
| `adapter` | 예 | | 지금은 `file`만 있다 |
| `datasets` | 예 | | 데이터셋 목록(1개 이상) |
| `name` | 아니오 | | 사람이 읽는 설명. 동작에 영향이 없다 |
| `collector` | 아니오 | `in_product` | 지금은 값을 검사하지도 쓰지도 않는다 |
| `limits` | 아니오 | 2.7의 기본값 | 엑셀 파일의 안전 한도 |

### 2.2 데이터셋

| 필드 | 필수 | 기본값 | 허용 값과 뜻 |
|---|---|---|---|
| `id` | 예 | | 데이터셋 이름. 소스 안에서 겹치면 안 된다. `-input <id>=<파일>`이 쓴다 |
| `parser` | 예 | | `csv`, `json_lines`, `xlsx` |
| `natural_key` | 예 | | 행을 구분하는 컬럼 이름의 목록(비어 있으면 안 된다). 이 컬럼들의 값(앞뒤 공백 제거)이 이어져 행의 이름이 된다 |
| `columns` | 예 | | 컬럼 선언(2.3). 1개 이상, 이름이 겹치면 안 된다 |
| `sheet` | 아니오 | 첫 시트 | 엑셀의 읽기 옵션(2.5). `xlsx`가 아닌 파서에 쓰면 거절한다 |
| `deletion` | 아니오 | `absent_in_full` | `absent_in_full`: 파일에서 계속 사라진 행은 원본 레코드에서 닫는다(6절). `none`: 사라져도 닫지 않는다 |
| `name` | 아니오 | | 설명. 동작에 영향이 없다 |
| `delimiter` | 아니오 | 쉼표 | CSV의 구분자. 쉼표(`comma`), 탭(`tab`), 세미콜론(`semicolon`), 세로선(`pipe`)만 되고, 이름 대신 글자(`,` `\t` `;` `\|`)도 된다. `csv`가 아닌 파서에 쓰면 거절한다 |
| `ignore_undeclared_columns` | 아니오 | `false` | `true`면 선언하지 않은 컬럼을 무시하고 `undeclared_column`을 보고하지 않는다 |
| `kind`, `receive_mode` | 아니오 | `raw`, `snapshot` | 지금은 이 값만 된다. 다른 값은 거절한다 |

자연 키가 비어 있는 행은 읽을 수 없는 줄(`unreadable_line`, `missing_natural_key`)이다. 자연 키가 같은 행이 둘이면 나중 행을 쓰고 앞의 행은 `duplicate_key` 문제로 남는다.

### 2.3 컬럼 선언

| 필드 | 필수 | 기본값 | 뜻 |
|---|---|---|---|
| `name` | 예 | | 매핑 정의가 쓰는 이름. 파일의 머리글과 같지 않아도 된다 |
| `paths` | 아니오 | `[name]` | 파일에서 이 컬럼을 찾는 이름의 목록. 앞에서부터 처음 있는 것을 쓴다. 머리글 이름이 바뀐 파일도 옛 이름을 뒤에 두면 읽는다 |
| `optional` | 아니오 | `false` | `true`면 파일에 없어도 된다(값은 빈 칸). 아니면 없을 때 데이터셋 전체를 쓰지 않는다(`shape_changed`). 메시지에 실제로 읽은 머리글 컬럼이 나온다 |
| `note` | 아니오 | | 정의를 읽는 사람을 위한 메모. 동작에 영향이 없다 |

선언하지 않은 컬럼이 파일 머리글에 있으면 `undeclared_column` 문제가 한 번씩 나온다. 쓰지 않을 컬럼도 선언해 두면 문제가 나오지 않는다. 컬럼이 많은 대장에서 일부만 쓸 때는 데이터셋에 `ignore_undeclared_columns: true`를 쓴다. 선언한 컬럼이 파일에서 사라진 것은 이 옵션과 상관없이 `shape_changed`로 계속 보고한다.

### 2.4 파일 종류별 읽는 규칙

- **CSV**: UTF-8만 읽고, 구분자는 `delimiter`로 정한다(기본 쉼표). 앞의 BOM은 있어도 된다. 첫 줄이 머리글이다. 머리글 이름은 글자 그대로(대소문자, 공백 포함) 맞아야 한다. 빈 줄은 건너뛴다. 필드 수가 머리글과 다른 줄은 `wrong_field_count`, 따옴표가 깨진 줄은 `malformed_csv`로 읽을 수 없는 줄이 된다.
- **JSON Lines**: 한 줄에 JSON 객체 하나. 빈 줄은 건너뛰고, 객체가 아니거나 한 줄에 둘 이상이면 `invalid_json`이다. `paths`는 점으로 이은 경로이고(`hardware.serial`) 숫자는 배열의 위치다(`tags.0`). 값은 비어 있지 않은 문자열, 숫자, 불리언만 된다. `null`, 객체, 배열, 빈 문자열은 값이 없는 것으로 본다. 이름에 점이 든 키는 가리킬 수 없다.
- **엑셀(`.xlsx`)**: 2.5. 머리글 이름은 앞뒤 공백을 없애고 연속한 공백을 하나로 줄여 맞춘다(대소문자는 구분한다). 수식은 파일에 저장된 계산 값을 읽는다. 날짜 칸은 `2026-03-14`, 날짜와 시각 칸은 `2026-03-14T09:30:00Z`, 정수는 `16`(`16.0` 아님), 불리언은 `true`와 `false` 글자로 바뀐다. 숫자 칸을 서식으로 `00123`처럼 보이게 한 경우 값은 `123`으로 읽으므로, 앞자리 0이 필요한 값은 칸을 텍스트로 저장한다.
- **한 파일의 한도**: 20MB. 넘으면 그 데이터셋은 쓰지 않고 `incomplete_capture`가 나온다.
- **읽지 못하는 것**: `.xls`("old binary Excel format"), 깨진 파일, 이름만 `.xlsx`인 CSV는 실행이 오류로 멈춘다. UTF-8이 아닌 CSV(EUC-KR, UTF-16)는 `ingest inspect`와 `ingest run` 모두 원인과 해결 방법을 말하고 멈춘다. 글자를 바꿔 읽지는 않는다(ADR-0046). 구분자가 다른 파일은 `delimiter`를 쓰고, `inspect`에는 `-delimiter`를 준다. 쉼표로 읽었더니 컬럼이 하나뿐이고 탭이나 세미콜론이 반복되면 `inspect`가 구분자를 확인하라고 알려 준다. 엑셀에서 "CSV UTF-8"로 저장하거나 `.xlsx`를 그대로 쓴다.

### 2.5 엑셀 시트 옵션 (`sheet`)

모두 선택이고, 아무것도 쓰지 않으면 첫 시트의 1행을 머리글로 보고 그 아래를 끝까지 읽는다.

| 필드 | 기본값 | 뜻 |
|---|---|---|
| `name` | | 시트 이름. 없는 이름이면 `shape_changed`로 보고하고 파일에 있는 시트 이름을 알려 준다(`ingest run` 요약 줄에는 `not_read`가 붙는다) |
| `index` | | 시트 번호(1부터). `name`과 함께 쓸 수 없다 |
| `header_row` | `1` | 머리글이 있는 행 |
| `header_rows` | `1` | 머리글의 행 수. 둘 이상이면 위에서 아래로 공백으로 이어 컬럼 이름을 만든다(`속성`과 `이름` → `속성 이름`) |
| `range` | 머리글 아래 끝까지 | 데이터 범위 `A5:F200`. 범위의 컬럼만 읽고, 머리글 행 아래에서 시작해야 한다 |
| `merged_cells` | `first_only` | 병합한 칸의 값을 `first_only`는 첫 칸에만, `fill`은 병합 범위의 모든 칸에 준다 |
| `stop_after_blank_rows` | `0`(끝까지) | 빈 행이 이만큼 이어지면 데이터가 끝난 것으로 보고 그 뒤는 읽지 않는다 |
| `skip_hidden_rows` | `false` | `true`면 숨긴 행을 읽지 않는다. 아니면 읽고, 읽은 수를 결과에 알려 준다(`hidden_rows_read`) |

- 읽는 컬럼이 모두 비어 있는 행은 항상 건너뛴다(빈 행). 숨긴 시트와 숨긴 컬럼도 읽는다. 차트 시트는 읽지 않는다.
- 선언한 컬럼의 칸이 `#N/A` 같은 오류이면 `cell_error`, 계산한 적 없는 수식(프로그램이 만든 파일에 흔하다)이면 `formula_without_value`가 나오고 그 칸은 빈 값이다. 행은 그대로 읽는다. 정제로 걸러지는 행(소계, 합계)의 칸도 문제로 나온다.
- 같은 엑셀 파일의 여러 시트를 데이터셋마다 `-input`으로 같은 파일에 잇는다.

### 2.6 소스 정의 초안 (`inspect -emit-source`)

`praxium ingest inspect -input 파일 -emit-source`는 살펴본 파일에 맞는 소스 정의 초안을 표준 출력에 쓴다(시트, 머리글 행, 컬럼, 추측한 값 종류는 `note`). 자연 키는 추측하지 않고 `FILL IN:`으로 시작하는 자리표시자를 둔다. 이대로 `ingest run`을 하면 어느 컬럼이 있는지 알려 주며 멈추므로 하나를 골라 바꾼다. CSV에는 `-delimiter`를 줄 수 있다.

### 2.7 한도 (`limits`)

엑셀 파일이 이 한도를 넘으면 실행이 오류로 멈추고 어느 한도인지 알려 준다. 필드 이름과 기본값은 `max_uncompressed_bytes`(268435456), `max_rows_per_sheet`(200000), `max_columns_per_sheet`(1024), `max_cell_chars`(32767), `max_shared_strings`(2000000)이다. 0보다 작으면 거절한다.

## 3. 매핑 정의 (mapping.json)

최상위는 `refinements`(선택), `feeds`(필수, 1개 이상), `links`(선택)이다.

### 3.1 정제 (`refinements`)

정제는 데이터셋의 행을 다듬어 새 이름을 붙인다. 컬럼은 늘거나 줄지 않는다. 피드와 다음 정제가 정제의 `id`를 데이터셋처럼 쓴다.

| 필드 | 뜻 |
|---|---|
| `id` | 정제의 이름. 비어 있거나 다른 데이터셋·정제와 겹치면 안 된다 |
| `input` | 입력. 데이터셋이나 앞에 나온 정제의 `id` |
| `ops` | 연산의 목록. 적은 순서대로 하나씩 적용한다 |

연산은 모두 `op`(연산 이름)와 `column`(입력에 있는 컬럼)을 가진다.

| `op` | 추가 필드 | 하는 일 |
|---|---|---|
| `filter` | `not_empty`, `in`, `not_in` 중 하나 이상 | 조건을 모두 통과한 행만 남긴다. `in`은 목록 안의 값, `not_in`은 목록 밖의 값이다(글자 그대로 비교하고 공백을 없애지 않는다). `not_empty`는 비었거나 공백뿐인 칸을 거른다 |
| `fill_empty` | `value`(비어 있으면 안 된다) | 비었거나 공백뿐인 칸에 `value`를 넣는다 |
| `replace` | `function`: `trim`, `lower`, `upper`, `map` | `trim`은 앞뒤 공백, `lower`와 `upper`는 대소문자. `map`은 `map` 필드(`{"운영": "on"}`)의 왼쪽 값을 오른쪽 값으로 바꾸고 목록에 없는 값은 그대로 둔다 |

걸러진 행은 결과의 `filtered`로 센다. 걸러진 행에서 이미 만든 객체는 그 피드가 `retires_objects`면 바로 닫힌다(실행해 확인했다).

### 3.2 피드 (`feeds`)

피드 하나는 데이터셋(또는 정제) 하나를 객체 타입 하나에 잇는다.

| 필드 | 필수 | 기본값 | 뜻 |
|---|---|---|---|
| `id` | 예 | | 피드 이름. 겹치면 안 된다 |
| `dataset` | 예 | | 읽을 데이터셋이나 정제의 `id` |
| `object_type` | 예 | | 객체 타입 이름(`Rack`, `PhysicalServer`). 목록은 4.3 |
| `basis` | 예 | | 값의 근거 종류. `configured`(설정, 설정에서 읽음), `observed`(관측, 수집한 순간에 그렇게 보임), `declared`(기재, 사람이나 문서가 적음), `derived`(파생, 규칙이 만듦). 엑셀 대장은 보통 `declared`이다 |
| `attributes` | 예 | | 컬럼을 속성에 잇는 목록. `attribute`(속성 이름), `column`, 선택 `rank` |
| `identity` | 예 | | 행이 어느 객체인지 알아보는 규칙(4절) |
| `identifiers` | 아니오 | | 객체에 붙일 식별자: `scheme`, `column`(4절) |
| `creates_objects` | 아니오 | `false` | `true`인 피드가 객체를 만든다. `false`인 피드는 다른 피드가 만든 객체에 값만 더한다. 대상 객체가 없는 행은 `value_without_object` |
| `retires_objects` | 아니오 | `false` | `true`면 이 피드가 만든 객체가 원본에서 사라졌을 때 닫는다. `creates_objects`와 함께만 쓴다 |
| `approximate` | 아니오 | `false` | `true`면 값의 근거 등급이 `assumed`(가정)이고 아니면 `fact`(사실)이다 |

- 만드는 피드를 먼저 처리하므로 피드의 순서는 상관없다.
- 빈 칸은 값이 없다는 뜻이다. 그 피드는 그 속성을 갖지 않고, 다른 피드의 값이 있으면 그것이 남는다.
- 같은 속성을 여러 피드가 주면 `rank`가 낮은 쪽이 이긴다(기본 `0`). 같으면 더 최근에 들어온 값, 그다음은 매핑에서 앞선 피드다. 진 쪽 값이 다르면 `source_conflict`로 알린다.
- 속성 `name`은 객체의 표시 이름이 된다. 값이 없으면 식별 키의 값이 표시 이름이다.
- 값 종류는 문자열, 열거(타입이 허용하는 값 그대로), 정수, 숫자, 불리언(`true`와 `false`만), 시각, 수량만 된다. 시각은 `2026-03-14T09:30:00Z` 형식이고 날짜만 있으면(`2026-03-14`) 그날 0시(UTC)로 본다. 목록과 맵 같은 속성은 매핑을 읽는 단계에서 거절한다.
- **칸 하나가 틀리면 그 행은 그 피드에서 통째로 쓰지 않는다.** `invalid_value`가 나오고 그 행의 객체는 만들어지지 않는다(다른 행은 계속 간다).
- 값이 자주 바뀌는 속성(`power_state` 같은 휘발성)은 이력 없이 마지막 값만 남고 결과의 `volatile`로 센다.

### 3.3 링크 (`links`)

링크 정의는 한 피드의 행에서 다른 피드의 행으로 간다. 객체가 된 행끼리만 잇는다.

| 필드 | 필수 | 기본값 | 뜻 |
|---|---|---|---|
| `id` | 예 | | 이름 |
| `link_type` | 예 | | 링크 타입 이름(`MOUNTED_IN`). 두 객체 타입 사이에 허용되는 링크여야 한다(4.3) |
| `from`, `to` | 예 | | `{"feed": "피드 id", "columns": ["컬럼", ...]}`. 양쪽 컬럼 수가 같아야 하고, 컬럼은 그 피드의 데이터셋에 있어야 한다 |
| `condition` | 예 | | 지금은 `same_key`만 된다 |
| `key_type` | 아니오 | 공백만 제거한 글자 | 키를 맞추기 전에 값을 다듬는 값 타입 이름(`praxium catalog value-types`). `serial_number`가 예이다 |
| `basis` | 아니오 | `derived` | 링크의 근거 종류. 근거 등급은 항상 `fact`이다 |

`from` 행의 키(컬럼 값들)와 같은 키를 가진 `to` 행의 객체로 링크를 만든다. 키 칸이 비어 있으면 링크를 만들지 않고 문제도 아니다. 같은 키의 행이 없으면 `link_target_missing`, 같은 키가 서로 다른 객체 둘 이상이면 `link_target_ambiguous`이다. 객체 한쪽에 하나만 허용하는 링크(`N:1`)가 둘 생기면 나중 것을 버리고 `cardinality`로 알린다.

## 4. 식별 키와 식별자

- **식별 키**는 객체가 "같은 것"임을 알아보는 값의 조합이다. 매핑의 `identity`가 정한다. 같은 키를 가진 행은 같은 객체이고, 다른 실행에서도 같은 객체이다.
- **식별자**는 객체에 붙는 바깥 번호(BMC MAC, 호스트 ID)이다. 웹 UI 상세의 "식별자"와 `praxium query get --include identifiers`가 보여 준다. 식별 키 가운데 `scheme` 방식인 것과 `identifiers`로 준 것만 식별자가 된다.

예제 Rack의 식별 키는 `asset_tag` 속성이라서 식별자는 비어 보인다. 비어 있는 것이 정상이다. 식별자를 채우려면 `identifiers`를 쓴다.

### 4.1 identity

`identity`는 규칙의 목록이다. 규칙마다 아래 둘 중 하나로 쓴다.

```json
{ "components": [ { "property": "manufacturer", "column": "mfr" },
                  { "property": "serial_number", "column": "serial" } ] }
{ "scheme": "host_id", "column": "host_id" }
```

`components`는 속성으로 된 식별 키이고 속성의 순서까지 타입의 키와 같아야 한다. `scheme`은 식별자 방식 하나로 된 키이다. 둘을 함께 쓰거나 둘 다 빼면 거절한다. 선택 필드는 `priority`(기본 0, 작은 것을 먼저 쓴다)와 `key_name`이다.

- 행은 칸이 모두 찬 규칙의 키를 만든다. 만들 수 있는 키가 없으면 `missing_identity`이다. 이미 아는 키가 있으면 그 객체이고, 없고 피드가 객체를 만들면 첫 키로 새 객체를 만든다.
- **`key_name`은 이름표가 아니라 키의 일부다.** 같은 객체를 여러 피드가 가리키려면 `key_name`이 같아야 한다(둘 다 쓰지 않거나 둘 다 같은 이름을 쓴다). 한쪽에만 있으면 서로 다른 키가 되어 값만 더하는 피드는 `value_without_object`가 된다(실행해 확인했다).
- 키가 둘이 서로 다른 객체를 가리키면 `identity_conflict`이다. 식별자 하나가 이미 다른 객체의 것이어도 같다.

### 4.2 identifiers

```json
"identifiers": [ { "scheme": "bmc_mac", "column": "mac" } ]
```

`scheme`은 식별자 방식 이름이고 칸의 값은 방식의 규칙으로 다듬어 저장한다. `bmc_mac`은 구분자를 없애고 소문자로 만든다(`02:00:5E:00:00:01`이 `02005e000001`이 된다). 조회할 때도 다듬은 값으로 찾는다. 그 타입에 맞지 않는 방식도 수집이 거절하지 않으므로(`PhysicalServer`에 `host_id`를 써도 오류가 없다) 타입에 맞는 방식은 4.3으로 확인한다.

### 4.3 타입에 허용되는 것 찾기

모델을 묻는 명령은 서버가 필요하다. `praxium demo`를 띄우고 출력된 주소와 토큰을 `--endpoint`, `--token`으로 준다.

```
praxium types list --kind object            # 객체 타입(--kind link, scheme, trait)
praxium types get Rack --include effective_properties --json
praxium types get MOUNTED_IN --kind link --json
praxium types get bmc_mac --kind scheme
praxium catalog value-types                 # 값 타입과 다듬는 규칙
```

- `effective_properties`에 `attribute`로 쓸 수 있는 속성이 이름, 값 타입(`value_type`), 열거 값(`enum_values`), 필수 여부와 함께 있다. `--include`가 없으면 타입이 직접 가진 속성만 나오고 `name`과 `manufacturer` 같은 공통 속성은 빠진다.
- `identity_keys`가 쓸 수 있는 식별 키이다. `ref`는 `pr.rack.asset_tag`처럼 `pr.<묶음>.<속성 이름>`이고 마지막 마디가 속성 이름이다. `scheme`이 있는 키가 `scheme` 방식이다. 구성 요소의 `kind`가 `context`인 키(Rack의 위치와 이름)는 파일 수집에서 쓸 수 없다. 이 키를 쓰려고 하면 `has no identity key made of the properties [name], in that order`가 나온다.
- 링크 타입의 `bindings`가 허용하는 쪽(`from`)과 받는 쪽(`to`)을 `trait`이나 `type`으로 알려 준다. 객체 타입의 `traits`에 그 `trait`이 있으면 허용된다(`PhysicalServer`는 `tr.rack_mountable`이 있어서 `MOUNTED_IN`으로 `Rack`에 갈 수 있다).

## 5. 실행 결과와 문제 코드

```
ingest source=… run=1 objects=4 links=0 volatile=4 … changed=true new_version=true version=… graph_hash=…
  dataset ds_servers collected=true received=4 unreadable=0 read=4 used=3 filtered=0 problem=1 unused=0 … sheet="서버 현황" … skipped_after_end=1 skipped_blank_row=1
  problems invalid_value=1
  problem invalid_value dataset=ds_servers key=S-0004 line=8 where='서버 현황'!A8:G8: …
completed with 1 problem (see "problem" lines above)
```

- `received`는 읽은 행(`read`)과 읽을 수 없는 줄(`unreadable`)의 합이고, `read`는 `used`(객체나 값이 된 행), `filtered`(정제가 거른 행), `problem`, `unused`(쓰는 피드가 없는 행)의 합이다.
- `skipped_blank_row`(빈 행), `skipped_hidden_row`(`skip_hidden_rows`로 뺀 숨은 행), `skipped_after_end`(`stop_after_blank_rows` 뒤라서 읽지 않은 행)은 문제가 아니고 `received`에 들어가지 않는다.
- 문제는 코드별로 세고 앞의 20개를 줄마다 보여 준다. 문제가 있어도 실행은 끝나고 종료 코드는 0이다. 코드로 막으려면 `-fail-on-problem`(종료 코드 20)을 쓴다.

| 코드 | 뜻 | 해결 |
|---|---|---|
| `shape_changed` | 파일에 선언한 컬럼이나 시트가 없어서 데이터셋 전체를 쓰지 않았다. 이전 실행의 데이터는 그대로다 | 메시지의 컬럼 이름(선언한 `name`)과 `paths`, `header_row`, 시트 이름을 맞춘다 |
| `undeclared_column` | 머리글에 있는데 선언하지 않은 컬럼 | 쓰지 않아도 `columns`에 선언하거나 `ignore_undeclared_columns`를 쓴다 |
| `unreadable_line` | 행으로 읽지 못한 줄. 이유는 `missing_natural_key`(자연 키 칸이 빔, 메시지가 빈 컬럼 이름을 준다), `wrong_field_count`, `malformed_csv`, `invalid_json`, `unreadable_header` | 이유대로 파일을 고치거나 자연 키를 바꾼다. `where`가 칸 주소를 준다 |
| `duplicate_key` | 같은 자연 키가 앞에도 있다. 나중 행을 썼다 | 키가 정말 유일한 컬럼을 `natural_key`로 쓴다 |
| `incomplete_capture` | 파일이 20MB를 넘거나 끝까지 읽지 못해 쓰지 않았다 | 파일을 나눈다 |
| `invalid_value` | 칸의 값이 속성 타입에 맞지 않아 그 행을 쓰지 않았다. 메시지에 허용 값이 있다 | 값을 `replace`의 `map`으로 바꾸거나 파일을 고친다 |
| `missing_identity` | 어떤 식별 키도 만들 수 없다(칸이 빔) | 칸을 채우거나 `identity`에 다른 키를 더한다 |
| `value_without_object` | 값만 더하는 피드의 행인데 같은 식별 키의 객체가 없다 | 만드는 피드의 키와 `key_name`을 맞춘다 |
| `source_conflict` | 같은 속성의 값이 피드마다 다르다. `rank`가 낮은 쪽을 썼다 | 믿을 피드의 `rank`를 낮춘다 |
| `identity_conflict` | 한 행의 키나 식별자가 서로 다른 객체를 가리킨다 | 원본의 중복이나 오타를 고친다 |
| `link_target_missing` | 링크의 키와 같은 행이 `to`에 없다(소스가 어긋남) | 원본의 값을 고치거나 링크 정의의 컬럼을 확인한다 |
| `link_target_ambiguous` | 같은 키의 행이 서로 다른 객체 둘 이상이다 | `to` 컬럼을 유일한 값으로 바꾼다 |
| `cardinality` | 링크 타입이 허용하는 수보다 많아 나중 링크를 버렸다 | 원본에서 한쪽에 하나만 남긴다 |
| `cell_error` | 엑셀 칸이 `#N/A` 같은 오류다 | 엑셀에서 고친다 |
| `formula_without_value` | 수식에 저장된 값이 없다(계산한 적 없음) | 엑셀에서 열어 저장한다 |

## 6. 다시 실행하면

`-data-dir`을 같은 폴더로 주면 이어서 실행한다. 행은 자연 키로 알아보고 내용이 같으면 `raw_unchanged`, 다르면 `raw_modified`로 세며, 달라진 객체의 속성만 바뀐다. 바뀐 것이 없으면 `changed=false`이고 새 버전은 만들지 않는다. `-input`을 주지 않은 데이터셋은 이전 그대로 둔다. 서버(`local`)가 폴더를 열고 있으면 `ingest run`은 거절한다. 소스가 다르면(다른 `id`) 한 폴더에 여럿을 넣어도 된다.

파일에서 사라진 행은 바로 닫지 않는다. `raw_unseen`으로 세다가 **3번의 실행 연속으로, 첫 실종에서 24시간이 지나서도** 없으면 원본 레코드를 닫는다(`deletion: none`이면 세지도 않는다). 닫힌 뒤에 그 행의 객체는 그 피드가 `retires_objects`일 때만 같이 닫힌다. 정제가 거른 행은 이 기다림 없이 바로 빠진다. 위 시간 조건은 코드로 확인했고 24시간 뒤의 동작은 실행해 보지 못했다.

## 7. 따라 하기: 엑셀 한 시트에서 정의 두 파일까지

지어낸 `servers.xlsx`가 있다고 하자. 시트 이름은 `서버 현황`이고 1행은 제목, 2행은 설명, 3행은 빈 행, 4행이 머리글이다.

| 관리번호 | 서버명 | 제조사 | 시리얼 | 상태 | BMC MAC | 랙 |
|---|---|---|---|---|---|---|
| S-0001 | web-01 | Zorvane | SN-A100 | 운영 | 02:00:5e:00:00:01 | R-01 |
| S-0002 | web-02 | Zorvane | SN-A200 | 유휴 | 02:00:5e:00:00:02 | R-01 |
| S-0003 | db-01 | Kelmar | SN-B100 | 운영 | 02:00:5e:00:00:03 | R-02 |
| S-0004 | db-02 | Kelmar | SN-B200 | 점검 | 02:00:5e:00:00:04 | R-02 |

표 아래에 빈 행과 `합계` 행이 있다. 먼저 `praxium ingest inspect -input servers.xlsx`로 본다. 머리글 행을 4로 맞게 짚는다(`header in row 4 (guessed…)`).

**1단계, 소스 정의.** 머리글이 4행이므로 `header_row`를 쓰고, 빈 행 뒤의 `합계`를 읽지 않게 `stop_after_blank_rows`를 쓴다. 컬럼 이름은 영어로 붙이고 `paths`에 엑셀 머리글을 둔다.

```json
{
  "id": "src_my_servers",
  "adapter": "file",
  "datasets": [{
    "id": "ds_servers",
    "parser": "xlsx",
    "sheet": { "name": "서버 현황", "header_row": 4, "stop_after_blank_rows": 1 },
    "natural_key": ["asset_no"],
    "columns": [
      { "name": "asset_no", "paths": ["관리번호"] },
      { "name": "name", "paths": ["서버명"] },
      { "name": "manufacturer", "paths": ["제조사"] },
      { "name": "serial", "paths": ["시리얼"] },
      { "name": "status", "paths": ["상태"] },
      { "name": "bmc_mac", "paths": ["BMC MAC"] },
      { "name": "rack", "paths": ["랙"] }
    ]
  }]
}
```

**2단계, 매핑 정의.** 서버의 식별 키는 제조사와 시리얼이다(4.3으로 확인한다). 상태는 `power_state`의 허용 값(`on`, `off`, `unknown` …)으로 바꾼다. 처음에는 `map`에 `운영`과 `유휴`만 적는다.

```json
{
  "refinements": [{
    "id": "rf_servers", "input": "ds_servers",
    "ops": [{ "op": "replace", "column": "status", "function": "map",
              "map": { "운영": "on", "유휴": "off" } }]
  }],
  "feeds": [{
    "id": "f_server", "dataset": "rf_servers", "object_type": "PhysicalServer",
    "creates_objects": true, "basis": "observed",
    "attributes": [
      { "attribute": "name", "column": "name" },
      { "attribute": "manufacturer", "column": "manufacturer" },
      { "attribute": "serial_number", "column": "serial" },
      { "attribute": "power_state", "column": "status" }
    ],
    "identity": [{ "components": [
      { "property": "manufacturer", "column": "manufacturer" },
      { "property": "serial_number", "column": "serial" } ] }],
    "identifiers": [{ "scheme": "bmc_mac", "column": "bmc_mac" }]
  }]
}
```

**3단계, 실행.** `-data-dir`을 빼면 결과를 남기지 않고 확인만 한다.

```
praxium ingest run -source source.json -mapping mapping.json -input ds_servers=servers.xlsx
```

```
ingest source=src_my_servers run=1 objects=3 links=0 volatile=3 … changed=true new_version=true …
  dataset ds_servers collected=true received=4 … used=3 filtered=0 problem=1 … sheet="서버 현황" sheet_state=visible skipped_after_end=1 skipped_blank_row=1
  problems invalid_value=1
  problem invalid_value dataset=ds_servers key=S-0004 line=8 where='서버 현황'!A8:G8: invalid_value: attribute power_state: invalid_value: "점검" is none of the values [on off powering_on powering_off unknown] of power_state
completed with 1 problem (see "problem" lines above)
```

`S-0004`의 상태 `점검`이 허용 값에 없어서 그 행을 쓰지 않았다. `skipped_*`는 빈 행과 `합계` 행이다. `map`에 `"점검": "unknown"`을 더하고 `-data-dir data`를 붙여 다시 실행하면 `completed without problems`가 나오고 객체 4개가 `data`에 남는다. 서버 4대가 보이는지, 식별자가 붙었는지는 `local`을 띄워 본다.

```
praxium local -data-dir data
praxium --endpoint <주소> --token <토큰> query find --type PhysicalServer
praxium --endpoint <주소> --token <토큰> query get --include identifiers --identifier bmc_mac:02005e000001
```

두 번째 명령은 서버 4대를, 세 번째는 `web-01`과 식별자 `bmc_mac 02005e000001`을 보여 준다. 식별자는 다듬은 값(구분자 없는 소문자)으로 찾는다.

## 8. 지금의 한계

- 소스는 파일(`.xlsx`, CSV, JSON Lines)뿐이다. CSV는 UTF-8만(구분자는 `delimiter`), `.xls`는 읽지 못한다.
- 정제 연산은 `filter`, `fill_empty`, `replace`뿐이고 컬럼을 새로 만들거나 합치지 못한다. 링크 조건은 `same_key`뿐이다.
- 파일 하나는 20MB까지이다.
- 파일 수집에서 쓸 수 있는 식별 키는 속성이나 식별자 방식으로만 된 것이다.
- 정의를 편집하는 화면은 없다. JSON을 직접 쓴다.
