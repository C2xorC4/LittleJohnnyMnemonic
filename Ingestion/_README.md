# Ingestion Protocol

This directory tracks the progress of ingesting technical and reference
books from the references directory (configured via `references_dir` in
`System/Config.md`, default `D:/References`) into LJM's Knowledge base.
One manifest per book. Every manifest is designed to **survive full
context loss** — a future session starts fresh, reads the manifest of
an in-progress book, and picks up at the documented next-chapter line.

## Quick Reference — Tooling

| Command | Purpose |
|---------|---------|
| `jm ingestion list` | Print the master index (`INDEX.md`) |
| `jm ingestion scan` | Find PDFs in the references directory without manifests |
| `jm ingestion scan --update` | Same, and record findings to INDEX.md |
| `jm ingestion sync` | Regenerate INDEX.md from current manifests |

- **`INDEX.md`** — the manifest-of-manifests. Lists every known book,
  its manifest status, next-action pointer, and any unmanifested PDFs
  discovered in the references directory. Regeneratable.
- **`manifest_<prefix>.md`** — one per book. Per-chapter progress,
  cross-section concept tracker, resume instructions.

## Core Principle: Signal-Efficient Ingestion

**Only document content that is either net-new to the system, or
conflicts with training-data priors.** Content that is already well-
represented in training data, or already covered by an existing vault
entry, does not get a new Knowledge entry. Existing entries may get
enriched with additional context or citations instead.

The reason: a Knowledge entry that duplicates training-data-level
knowledge wastes retrieval context and slots without adding signal.
LJM's value is in filling gaps, not replicating what Claude already
knows.

### Signal assessment — apply per chapter / per major concept

After reading, ask of each substantive concept:

1. **Is this in training data at the level the book presents?** If yes
   and the book's framing is consistent with training → **skip**. Note
   in manifest that the chapter was read but content was already known.
2. **Is there an existing vault entry covering this?** If yes →
   **enrich** the existing entry with the book's additional context,
   specific examples, or citations. Don't create a parallel entry.
3. **Is it net-new to both training and vault?** → **create** a new
   Knowledge entry.
4. **Does it conflict with training-data priors?** → **create** with
   `training_override: true` in frontmatter. High-signal.

A chapter can produce zero, one, or multiple Knowledge entries
depending on its signal density. A 40-page chapter of known material
might produce no entries. A 10-page chapter with a novel framework
might produce three.

## Full-Context Preservation — default mode

Unless the user specifies "minimal summary" for a specific ingestion,
new Knowledge entries should be **comprehensive** — 300-600+ lines for
substantial chapters, not 100-200 line distillations. Preserve:

- The chapter's structural argument as the author frames it
- Specific case studies walked through, not just named
- Direct quotations of key formulations (attributed inline)
- Concrete data, dates, named entities, specific page references
- Examples, counterexamples, and the reasoning that links them
- Cross-references to other chapters, books, or vault entries

The point: future retrieval should surface entries that can carry their
own weight, not entries that point at a book the user has to re-read.

## Workflow

### Starting or resuming ingestion

1. Read this `_README.md` if unfamiliar with the protocol.
2. List `Ingestion/manifest_*.md` to see active ingestions.
3. Pick the manifest whose `status` is `in-progress`, or whose
   "Next chapter" resume pointer is unambiguous.
4. Read the target chapter from the `source_pdf` via `pdftotext`:
   ```
   pdftotext -f <start_page> -l <end_page> "<source_pdf>" -
   ```
   Note the manifest's chapter table uses book-internal page numbers
   (as printed in the book), which usually differ from PDF page
   numbers by a small offset for front-matter. If extraction returns
   wrong content, increment by the offset until it matches.

### During chapter processing

5. **Signal assessment** — identify which concepts are net-new,
   which enrich existing entries, which can be skipped as known.
6. For net-new concepts: draft `Memory/Knowledge/<prefix>_<topic>.md`
   with full-context body and proper `source_document` attribution.
7. For enriching existing entries: edit the existing file, add a
   dated refinement section (e.g., `## Refinement from <book> Ch. X
   (2026-04-14)`) with the new material.
8. Update the manifest's chapter row:
   - `status`: drafted → covered (when cross-refs are done)
   - `action`: filename(s) of new entries, or `enrich:<target>`,
     or `skipped-known`
   - `touched`: today's date
9. If a concept crosses multiple chapters, note it in the Cross-Section
   Concept Tracker. Decide whether to resolve now (chapter's concept
   fully matures here) or defer to end-of-book.

### End-of-book

10. When all chapters are `covered` or `skipped-known`, review the
    Cross-Section Concept Tracker.
11. Produce any cross-cutting Knowledge or Semantic entries that
    synthesize patterns spanning multiple chapters.
12. Update manifest's top-level `status` to `completed`.

## Manifest Structure

Every manifest uses consistent frontmatter and sections. Do not deviate
without updating this README so the protocol stays uniform across books.

### Frontmatter fields

```yaml
type: ingestion-manifest
book_title: "<Title>"
book_author: "<Author>"
book_year: <year>
book_publisher: "<Publisher>"
isbn: "<ISBN if known>"
license: "<copyright | public | open-access>"  # see note below
source_pdf: "<absolute path>"
total_pages: <int>
prefix: "<2-4 char filename prefix for entries>"
ingestion_started: <date or "-">
last_touched: <date>
status: planned | in-progress | completed
```

The `license` field informs the sharing-policy boundary:
- **copyright**: summaries and transformative synthesis only, verbatim
  redistribution prohibited
- **open-access**: author has licensed free redistribution (e.g.,
  Bonanno's Game Theory), fewer constraints
- **public**: public-domain, no copyright constraints

### Required sections

1. **Chapter Status table** — one row per chapter/appendix.
2. **Cross-Section Concept Tracker** — empty initially, populated as
   cross-cutting patterns are discovered.
3. **Resume Instructions** — human-readable "next action" pointer that
   survives context loss. Updated after every ingestion session.

### Chapter Status columns

| Column | Purpose |
|---|---|
| `Ch` | Chapter number or letter (e.g., 1, A, B) |
| `Title` | Chapter title |
| `Pages` | Book-internal page range |
| `Status` | `planned` / `reading` / `assessed` / `drafted` / `covered` / `skipped-known` |
| `Action` | Target entry filename, `enrich:<existing>`, or `skipped-known` |
| `Touched` | Date of last update |

### Status transitions

```
planned ──read──> assessed ──draft──> drafted ──review──> covered
   │                  │
   │                  └──known/covered elsewhere──> skipped-known
   │
   └──skip (user-directed)──> skipped-known
```

## Entry Naming

Filename prefix per book is defined in the manifest's `prefix` field.
Current assignments:

| Prefix | Book |
|---|---|
| `acw` | The Art of Cyberwarfare (DiMaggio, 2022) |
| `ootm` | Out of the Mountains (Kilcullen, 2013) |
| `gt` | Game Theory, 3rd ed. (Bonanno, 2024) |

New entries go to `Memory/Knowledge/<prefix>_<topic>.md` where
`<topic>` is a descriptive snake_case identifier (not the chapter
number). Example: `ootm_theory_of_competitive_control.md`, not
`ootm_ch3.md`. This keeps entries retrieval-friendly by topic rather
than book-structure-dependent.

## Sharing Policy Reminder

Per `Buffer/2026-04-14_vault-sharing-policy.md` (eventually
consolidated into Feedback):

- Transformative summaries with citation are **shareable** for
  copyright-protected books
- Full-text reproductions are **not shareable**
- Open-access sources like Bonanno's Game Theory have fewer
  constraints but still warrant proper attribution

Apply the sharing-policy boundary to ingested content the same way it
applies to the rest of the vault.

## Adding a New Book

1. Create `Ingestion/manifest_<prefix>.md` following the frontmatter
   and section structure above.
2. Populate the chapter table from the book's TOC.
3. Set `status: planned` and record `ingestion_started: -`.
4. Update this README's entry-naming prefix table.
