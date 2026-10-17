---
name: cumin-trace-docs
description: Write the requirement documents and the design documents of this repository with a Maintainer, in the format of cumin-trace. Use before you write or change a page under the requirement documents, the source code structure, or a design document.
---

# Write requirement and design documents

This skill helps you write the documents of this repository with a Maintainer. The documents are the source of truth of the product. cumin-trace reads them and checks their format.

The requirement documents are in the directory `requirements_dir` of `.cumin-trace.toml`. The default is `docs/ja/requirements`. The source code structure is `docs/ja/architecture/overview.md`. A design document is one directory for one feature under `docs/ja/designs/`.

## Before you write

1. Read the format of the requirement documents: <https://github.com/cloveclovedev/cumin-trace/blob/main/docs/ja/requirements/format/index.md>. Follow it for every requirement.
2. Read the glossary of this repository: `glossary.md` in the requirement documents.

## Templates

The templates are in the directory `templates/` next to this file.

| Kind | Template | Form |
|---|---|---|
| Index | `templates/entry.md` | Set by cumin-trace |
| Overview | `templates/overview.md` | Set by cumin-trace |
| Area page | `templates/area.md` | Set by cumin-trace |
| Glossary | `templates/glossary.md` | Set by cumin-trace |
| Backlog | `templates/backlog.md` | Set by cumin-trace |
| Technology page | `templates/technology.md` | Recommended |
| Source code structure | `templates/architecture.md` | Recommended |
| Design document: entry (`index.md`) | `templates/design-index.md` | Recommended |
| Design document: detail (one file for each topic) | `templates/design-detail.md` | Recommended |

- A kind whose form cumin-trace sets: start each new document of that kind from its template.
- A recommended kind: when the repository already has a document of that kind, write the new document in the form of the existing one. Start from the template only when the repository has none.
- A comment `<!-- ... -->` in a template says what to write there. Replace each comment with the text that it asks for.
- The template of the area page holds an area in the heading and in every ID. That area is `<AREA>`, or the first area of the repository, for example `TASK`.
- For a new area, follow these steps:
  1. Decide the area with the Maintainer.
  2. Add the area to `areas` in `.cumin-trace.toml`.
  3. Add a row for the new area to the table of areas on the entry page, `<requirements_dir>/index.md`.
  4. Write the page to `<requirements_dir>/<area in lower case>/index.md`. For the area `TASK`, the page is `task/index.md`.
  5. Replace the area of the template with the new area in every ID. Rewrite the heading as the comment of the template says.
  6. Draw the diagram of the area as `overview.puml` in the same directory. Render it to `overview.svg` in that directory.

## While you write

- Use each term of the glossary exactly as the glossary writes it.
- Never use a wording from the column 使わない言い方 (wordings to avoid) of the glossary.
- Give a new requirement the sequence number after the largest one of its area. Count the numbers of deprecated requirements too. Never use an ID again.

## After you write

1. Run `cumin-trace check` at the top of the repository.
2. Fix every error that the check prints.
3. Run the check again until it prints no error.
