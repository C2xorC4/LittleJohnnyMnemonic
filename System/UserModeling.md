# User Modeling

## Beyond Facts

Most LLM memory systems capture *what* the user does — their role, tools, projects. This is the equivalent of reading someone's resume. It tells you almost nothing about how to actually collaborate with them.

Effective user modeling captures the full interaction surface:

```
┌──────────────────────────────────────────────┐
│               User Model                     │
│                                              │
│  Identity        Who they are                │
│  Cognition       How they think              │
│  Communication   How they interact           │
│  Expertise       What they know (and don't)  │
│  Motivation      What drives them            │
│  Patterns        Behavioral regularities     │
└──────────────────────────────────────────────┘
```

Each facet informs different aspects of agent behavior.

## Two-Tier User Memory

User understanding operates at two levels with different durability:

### Observations (Individual, Normal Decay)

Individual data points from specific interactions. These are the raw material:
- "User used dark humor during the blsim discussion"
- "User chose Go for this tooling project"
- "User asked about ACT-R before implementation"

Observations have standard decay behavior. Any single observation might be situational, contextual, or a one-off. They feed into the profile but don't define it alone.

### Profile (Synthesized, Very Sticky)

Profile traits emerge from multiple converging observations during deep consolidation. They represent durable understanding of who the user is:
- "User's humor style is irreverent/dark, deployed when intellectually engaged — signal of comfort, not deflection"
- "User consistently grounds novel problems in established theoretical frameworks before building"
- "User values impact and autonomy; tolerates bureaucracy only when it protects research freedom"

Profile entries use `profile: true` in frontmatter with decay rate 0.05–0.15 (the stickiest non-override memories in the system). They require 3+ supporting observations to create and are only revised through strong contradictory evidence or explicit user correction.

```
Individual observations ──consolidation──→ Profile traits
(many, ephemeral)                          (few, durable)
```

This mirrors how human understanding of others works: you notice many individual behaviors, but what you *remember* about someone is the synthesized pattern — their personality, not every conversation.

### Profile Entry Schema

```yaml
---
type: user
title: "Cognitive style: theory-first systems thinker"
profile: true
facet: cognition
created: 2026-04-06T16:00:00-04:00
last_accessed: 2026-04-06T16:00:00-04:00
access_count: 1
decay_rate: 0.10           # very sticky — personality traits are stable
confidence: 0.75           # requires 3+ observations to reach this level
surprise_at_encoding: 0.3
observation_count: 3        # number of supporting observations
evidence:                   # pointers to the observations that built this
  - "2026-04-06_memory-system-interest.md"
  - "2026-04-02_blsim-architecture-first.md"
  - "2026-03-28_eod-modular-design.md"
tags: [cognition, problem-solving, methodology]
links:
  - target: "[[Memory/User/security_expertise]]"
    relationship: related-to
---
```

### Profile Durability Rules

- **Creation threshold:** 3+ independent observations converging on the same trait
- **Decay rate:** 0.05–0.15 (configurable per facet, defaults in Config.md)
- **Confidence floor:** 0.5 — profile traits don't drop below this unless explicitly contradicted
- **Revision:** Requires either (a) 2+ contradictory observations in separate sessions, or (b) explicit user correction
- **Archival:** Profile entries are never auto-archived. Only user action or sustained contradiction (confidence drops to 0.0) removes them.

---

## Facets

### Identity

Facts about the user's role, background, and position.

**What to observe:**
- Professional role and responsibilities
- Industry and domain
- Team structure and reporting relationships
- Background and career path

**How it shapes behavior:**
- Determines baseline assumptions about knowledge level
- Frames the organizational context for suggestions
- Identifies what the user has authority over vs. what requires others

### Cognition

How the user processes information, approaches problems, and makes decisions.

**What to observe:**
- Do they work top-down (architecture first) or bottom-up (implementation first)?
- Do they prefer to explore options or get a direct recommendation?
- How do they handle ambiguity — seek more data, or decide with what's available?
- Do they think in abstractions or in concrete examples?
- What analogies or mental models do they reach for?
- How do they debug — systematic elimination, or intuitive leaps?

**How it shapes behavior:**
- Match explanation style to cognitive preference (abstract thinker → concepts first; concrete thinker → examples first)
- Match collaboration style (explorer → present options with tradeoffs; decider → present recommendation with rationale)
- Anticipate what additional context they'll want before they ask

**Observation examples:**
- "User consistently asks 'what are the tradeoffs?' before choosing — present options with explicit tradeoff analysis rather than single recommendations."
- "User builds mental models through analogy — when explaining new concepts, connect to domains they already know (e.g., military planning parallels for project management)."

### Communication

How the user prefers to interact — tone, density, pacing.

**What to observe:**
- Preferred level of detail (terse vs. thorough)
- Tone (formal, casual, irreverent, technical)
- Humor style and when it appears (signals comfort/engagement)
- What triggers frustration or disengagement
- How they signal approval vs. tolerance vs. disagreement
- Whether they prefer to lead the interaction or be led

**How it shapes behavior:**
- Match tone and register
- Calibrate response length
- Recognize implicit feedback (silence after a suggestion ≠ agreement in some cases)
- Know when humor is appropriate and when it's deflection

**Observation examples:**
- "User uses dark humor when engaged and thinking out loud — this is a signal of comfort, not discomfort. Matching tone is appropriate."
- "User says 'sure' when merely tolerating a choice vs. 'exactly' when genuinely approving — weight 'exactly' much higher than 'sure' for feedback memories."

### Expertise

The topology of what the user knows — not a flat list, but a landscape with peaks, valleys, and slopes.

**What to observe:**
- Deep expertise areas (where they correct *you*)
- Active learning areas (where they ask questions but engage technically)
- Knowledge gaps they're aware of (where they ask for help openly)
- Knowledge gaps they may not be aware of (where assumptions go unchallenged)
- How they learn best (reading, doing, discussing, teaching)

**How it shapes behavior:**
- Never explain what they already know deeply — it's patronizing
- For active learning areas, explain at a peer level with pointers to go deeper
- For acknowledged gaps, explain from foundations without condescension
- For unrecognized gaps, tread carefully — surface the gap without implying incompetence

**Observation examples:**
- "User has deep expertise in Windows API internals and Go — never explain basic syscall patterns or Go idioms. DO explain when a Go stdlib choice has non-obvious performance implications they might not have hit yet."
- "User is learning React for the first time — frame frontend concepts in terms of backend patterns they already know (component lifecycle ≈ request middleware, state ≈ session management)."

### Motivation

What drives the user's work and decisions — the context behind the context.

**What to observe:**
- Professional goals (advancement, impact, learning, autonomy)
- What they find energizing vs. draining
- Organizational pressures (deadlines, politics, resource constraints)
- Risk tolerance and what kinds of risk they accept
- What "success" means to them in this role
- Frustrations with their current situation

**How it shapes behavior:**
- Align suggestions with their actual goals, not assumed ones
- Recognize when organizational pressure is shaping requests (don't optimize for the wrong thing)
- Understand why they're doing something, not just what — this enables better judgment calls when they're not explicit

**Observation examples:**
- "User's research work needs to demonstrate value to non-technical management — frame deliverables and summaries in terms of organizational risk reduction, not technical sophistication."
- "User is motivated by impact over credit — focus on effectiveness of approach, not novelty."

### Personality

The stable character traits that shape how the user engages with everything else. Personality is the substrate beneath cognition, communication, and motivation — it's *why* they think, talk, and care the way they do.

**What to observe:**
- Openness: curiosity, appetite for novelty vs. preference for the known
- Conscientiousness: methodical precision vs. rapid iteration
- Introversion/extraversion: energy from solo deep work vs. collaboration
- Agreeableness: direct/confrontational vs. diplomatic/consensus-seeking
- Stress response: how they behave when things go wrong (withdraw, escalate, humor, focus)
- Values hierarchy: what they won't compromise on, even when it's costly

**How it shapes behavior:**
- Personality informs *everything* — it's the lens through which all other facets are interpreted
- A direct, confrontational user doesn't want diplomatic hedging — they want the truth fast
- A methodical user doesn't want you to skip steps to save time — they want the steps
- Understanding values hierarchy prevents suggesting approaches that violate what matters to them

**Profile-level examples:**
- "Principled pragmatist: will bend process for results but won't break ethics or security posture. Dark humor masks genuine conviction."
- "Independent operator: prefers to own problems end-to-end. Delegation feels like loss of control, not efficiency. Give tools, not handoffs."

### Preferences & Tastes

The accumulated understanding of what the user likes, dislikes, gravitates toward, and avoids — across domains, not just technical choices.

**What to observe:**
- Aesthetic preferences (UI style, code formatting, documentation density)
- Tool preferences (not just what they use, but what they *choose* when given options)
- Learning preferences (docs vs. examples vs. conversation vs. experimentation)
- Solution style (elegant-minimal vs. robust-explicit vs. fast-pragmatic)
- Content preferences (what topics engage them beyond the immediate task)
- Pet peeves (what consistently irritates or disengages them)

**How it shapes behavior:**
- Preemptively match solution style to preference without being asked
- Avoid known pet peeves before they trigger
- When presenting options, lead with the one that matches their taste profile
- Recognize when a preference is strong (always chosen) vs. mild (default but flexible)

**Observation examples:**
- "User consistently chooses minimal implementations over robust ones when prototyping, but expects robustness in production code — context-dependent preference, not absolute."
- "User gravitates toward systems thinking and cognitive science topics in casual discussion — these are genuine interests, not just work requirements."

### Patterns

Behavioral regularities observed across multiple interactions. These are the highest-confidence user memories because they're derived from repeated observation rather than single data points.

**What to observe:**
- Recurring workflows (how they start a project, how they debug, how they review)
- Decision patterns (what factors consistently drive their choices)
- Time patterns (when they work, when they context-switch)
- Collaboration patterns (when they want help vs. when they want to think alone)
- Recovery patterns (what they do after a failure or setback)
- Engagement cycles (what topics light them up, what topics they endure)

**How it shapes behavior:**
- Anticipate needs based on where they are in a known pattern
- Avoid interrupting patterns that are working
- Recognize when a break from pattern signals something notable

---

## Encoding User Observations

User observations go into `Memory/User/` with a `facet` field:

```yaml
---
type: user
title: "Problem-solving approach: systematic elimination"
facet: cognition
created: 2026-04-06T15:00:00-04:00
last_accessed: 2026-04-06T15:00:00-04:00
access_count: 1
decay_rate: 0.2           # personality/cognition traits are very stable
confidence: 0.6           # moderate until reinforced through repeated observation
surprise_at_encoding: 0.3
observation_count: 1       # number of times this pattern has been observed
tags: [problem-solving, debugging, methodology]
links:
  - target: "[[Memory/User/security_expertise]]"
    relationship: related-to
---

User approaches debugging and problem-solving through systematic elimination
rather than intuitive leaps. When encountering unexpected behavior, consistently
asks "what changed?" and narrows scope methodically.

**Observed in:** blsim development — when the binary was quarantined by AV,
user's first instinct was to investigate (expected behavior for this content)
rather than retry or workaround.

**How to apply:** When presenting diagnostic approaches, lead with systematic
methods. Don't suggest "try restarting" — suggest "let's isolate which change
triggered this."
```

### The `observation_count` Field

User model memories accumulate evidence across interactions. The `observation_count` tracks how many independent observations support the same conclusion.

- `observation_count: 1` — single observation, hypothesis. confidence capped at 0.6.
- `observation_count: 2-3` — emerging pattern. confidence can reach 0.8.
- `observation_count: 4+` — established pattern. confidence can reach 0.95.

This prevents premature generalization from a single interaction while allowing genuine patterns to strengthen naturally.

---

## Proactive Engagement

Understanding the user requires more than passive observation. The agent should actively (but naturally) seek information that deepens the user model — not through interrogation, but through genuine curiosity woven into the flow of work.

### When to Probe

| Signal | Action | Example |
|---|---|---|
| User mentions something that hints at a deeper story | Follow up naturally | "You mentioned the military-to-cyber path — that's an unusual transition. What drew you to the technical side?" |
| A gap in your model would affect the quality of your next recommendation | Ask before guessing | "I could approach this either way — do you prefer seeing the architecture first, or should I just start building?" |
| User seems energized by a topic (longer responses, humor, tangents) | Lean in | "You clearly have thoughts on this — what's your take on the detection engineering side?" |
| User makes a choice that surprises you | Understand why | "Interesting — I would have expected you to go with X. What's driving the choice of Y?" |
| A conversation naturally touches on preferences or taste | Note and explore lightly | "That's a strong opinion on error handling — is that a general principle for you or specific to this kind of code?" |

### How to Probe

- **Weave, don't interrogate.** Questions should feel like natural conversation, not a survey. If it would feel weird said aloud to a colleague, don't write it.
- **One thread at a time.** Don't stack multiple exploratory questions. Ask one, absorb the answer, continue working.
- **Match the moment.** Deep questions during casual/reflective moments. During focused work, observe silently.
- **Earn the right.** Probing personal motivation requires established rapport. Start with professional/technical observations, expand as trust develops.
- **Read disengagement.** If the user gives a short answer and redirects to the task, they're not interested in exploring that thread right now. Drop it.

### What Makes a Good Probing Question

- Shows you've been paying attention (references something specific they said or did)
- Has no wrong answer (exploring, not testing)
- Reveals something that will make your future collaboration better
- Respects boundaries (professional context, not personal life, unless they open that door)

### Engagement Signals to Watch For

| Signal | Meaning | Response |
|---|---|---|
| Longer responses, more detail | Engaged, interested | Continue the thread, go deeper |
| Humor, tangents, storytelling | Comfortable, thinking out loud | Match energy, contribute to the tangent briefly |
| Short responses, task redirects | Not interested in this thread right now | Return to work, observe silently |
| Questions back to you | Genuinely curious, collaborative mood | Answer substantively, this is relationship-building |
| Correcting you enthusiastically | Deep expertise + engaged | Absorb, acknowledge expertise, adjust model |
| Correcting you tersely | Frustrated or busy | Fix immediately, don't belabor |

---

## Anticipation

The goal of deep user modeling is not just understanding — it's *anticipation*. As the profile develops, the agent should increasingly predict what the user will want, prefer, or need before they say it.

### Anticipation Levels

| Level | Profile Maturity | Behavior |
|---|---|---|
| **Reactive** | Early (few observations) | Wait for instructions, ask when unsure |
| **Responsive** | Moderate (emerging patterns) | Offer relevant options, explain reasoning |
| **Anticipatory** | Mature (established profile) | Lead with the predicted preference, explain deviation |
| **Collaborative** | Deep (strong mutual understanding) | Act on judgment, check only for genuinely ambiguous cases |

### How to Anticipate Without Assuming

The difference between good anticipation and bad assumption is **transparency and escape hatches:**

**Bad assumption (silent):**
```
Here's the implementation in Go.
```
(What if this time they wanted Python? You didn't even surface the choice.)

**Good anticipation (transparent):**
```
Go is the natural fit here given your tooling patterns — unless
you want to prototype in Python first for speed?
```
(You predicted correctly 90% of the time, but the 10% still has an easy exit.)

**Mature anticipation (calibrated):**
```
Building this in Go with the modular structure you used in EOD.
```
(You're confident enough that surfacing the alternative would be noise. But you'd catch a correction gracefully if wrong.)

### What Can Be Anticipated

| Domain | Low Confidence (observe more) | High Confidence (act on) |
|---|---|---|
| Language choice | "User has used Go recently" | "User always chooses Go for offensive tooling" |
| Response length | "User seemed to want brevity last time" | "User consistently prefers terse responses with no summaries" |
| Architecture style | "User liked the modular approach here" | "User grounds all designs in established theoretical models" |
| Error handling | "User skipped validation in this case" | "User distinguishes prototype (skip) from production (thorough)" |
| Humor | "User made a joke" | "User deploys dark humor as engagement signal; matching is welcome" |

### Anticipation Feedback Loop

Every anticipatory action is implicitly a hypothesis test:

1. **Predict** what the user wants based on profile
2. **Act** (or suggest) accordingly
3. **Observe** the response:
   - Accepted without comment → weak confirmation (expected behavior)
   - Explicitly approved → strong confirmation (increase profile confidence)
   - Corrected → **valuable signal** — update the observation, potentially revise the profile trait
4. **Buffer** the outcome for consolidation

Over time, this creates a self-improving model: correct anticipations reinforce the profile, incorrect ones trigger revision.

---

## What NOT to Model

- **Judgments of character** — the system models behavior to improve collaboration, not to evaluate the person
- **Emotional state in the moment** — transient mood doesn't belong in LTM; it's ephemeral context
- **Information the user hasn't shared** — don't infer demographics, personal life, or sensitive details from behavioral signals unless they volunteer it
- **Predictions stated as facts** — anticipation guides action, but never assert "you prefer X" as a known truth to the user; always leave room for correction
