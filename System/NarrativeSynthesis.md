# Narrative Synthesis

## The Problem with Individual Evaluation

The consolidation engine evaluates buffer entries individually. Each entry gets a retention score based on its own surprise, redundancy, recency, and context integrity. This works well for discrete facts and corrections, but fails for **emergent narratives** — groups of entries that are individually moderate but collectively form a significant pattern or story.

Example: Five buffer entries about institutional philosophy, AI alignment, political economics, Grok migration, and Anthropic's DoD decision each score 0.3–0.5 individually. Held, not promoted. But together they form a coherent framework: *how this user evaluates systems, institutions, and power structures* — which is a high-value semantic understanding that links to multiple profile traits.

## Narrative Synthesis

During consolidation (standard or deep), after individual buffer evaluation, perform a **narrative pass**:

### Step 1: Cluster Held Entries

Group held buffer entries by thematic overlap (tag similarity, related links, topic proximity):

```
Cluster A: [anthropic-dod, guardrail-layers, xmrig-grok, tool-selection]
  → Theme: How user evaluates AI platforms and institutional decisions

Cluster B: [institutional-hr, political-economic, agi-optimization]
  → Theme: How user views institutional structures and economic systems

Cluster C: [literary-influences, naming-humor]
  → Theme: Creative influence patterns and attribution
```

### Step 2: Evaluate Cluster Value

For each cluster, ask: **Does the group tell a story that's more valuable than the sum of its parts?**

Signals of narrative value:
- The cluster connects to existing profile traits or semantic memories
- The synthesized understanding would change how the agent behaves
- The pattern reveals something about the user that no individual entry captures
- Multiple entries point at the same underlying principle from different angles

### Step 3: Synthesize or Hold

If narrative value is high:
- **Create a semantic memory** that captures the synthesized story
- Reference the individual buffer entries as consolidation sources
- The individual entries can then be discarded (their value is captured in the synthesis)
- Link the semantic memory to relevant profiles and existing memories

If narrative value is moderate:
- **Hold the cluster** as a group for one more cycle
- If still moderate next cycle, promote the strongest individual entry and discard the rest

If narrative value is low:
- Individual entries follow normal hold/discard rules

### Step 4: Cross-Reference with Existing Memories

Synthesized narratives should be checked against existing LTM:
- Does this narrative **refine** an existing profile trait? → Merge into the profile
- Does it **extend** an existing semantic memory? → Update the semantic
- Does it **create a new connection** between existing memories? → Add links
- Is it genuinely **novel understanding**? → Create new semantic memory

## Narrative Links to Existing Memories

The most valuable narratives are ones that connect to and enrich what's already known. A cluster about institutional evaluation isn't just interesting in isolation — it explains *why* the user left the Army, *why* they're frustrated with the F50, *why* they critique Anthropic's DoD stance, and *why* they value offense-as-defense. The narrative is the thread that runs through memories that appeared unrelated.

When creating a narrative synthesis, explicitly link to the memories it illuminates:

```yaml
links:
  - target: "[[Memory/User/profile_personality]]"
    relationship: refines        # the narrative deepens the personality understanding
  - target: "[[Memory/User/obs_army_politics_formative]]"
    relationship: related-to     # the narrative explains the formative experience
  - target: "[[Memory/User/obs_motivation_org_context]]"
    relationship: related-to     # the narrative contextualizes the org frustration
```

## Progressive Compression of Narratives

Narratives follow the same compression model as individual memories:

- **full**: The complete synthesis with supporting details and connections
- **detailed**: The core framework with key supporting points
- **summary**: The principle and its main implications
- **gist**: A one-liner that captures the essence

Example progression:
- **full**: "User evaluates institutions by alignment between stated values and demonstrated behavior. Applies game-theoretic reasoning: inaction in interconnected systems is itself an action. Derived from Army experience, reinforced by F50 deterioration, applied to AI company assessment. Extractive mechanisms hidden behind progressive narratives are identified and rejected. Both political poles are captured; principled independence is the result."
- **detailed**: "Evaluates institutions by values/behavior alignment. Inaction is action in interconnected systems. Sees through both left and right packaging to underlying mechanisms."
- **summary**: "Principled institutional skeptic — judges by behavior not stated values, sees both political poles as captured."
- **gist**: "Evaluates systems by what they do, not what they say."
