# RULINGS (the oracle): short, FINAL, with their authority

Only a RULING in this file is authority: the owner's, or coord-fs3's strictly under the owner's rules. The owner can overrule anything, including any oracle test, at any time; that becomes a ruling (and the departure is recorded per D4).
openciv3 (the C# fork) and civ3.exe research are both INCOMPLETE: they are evidence that informs a ruling, never a master. The `source` column says what a ruling's basis is.
No ruling for your question? Follow the plan text; if the plan is silent, choose the simplest behaviour that satisfies it, say so in your report, and do not block on research. If notes disagree with this file, this file wins.

| id | topic | ruling | source | authority | areas | date |
|---|---|---|---|---|---|---|
| R-001 | Who decides (authority) | The owner's ruling is final and overrides everything, including the C# oracle tests and civ3.exe research; where nobody has ruled, use the best evidence (civ3.exe CONFIRMED, then the C#; a civ3.exe GUESS/PARTIAL row follows the C#) and say so | custom | D2, D2a, D4 (decisions.md) | rules,oracle | 2026-11-22 |
| R-002 | Allow-list entries | An oracle allow-list entry may cite EITHER a CONFIRMED civ3.exe address OR an owner decision Dn; no other reason is accepted | custom | D4 (owner 11-22) | oracle,divergences | 2026-11-22 |
| R-003 | Corruption: distance trade factor | The distance calculation keeps ConnectedToCapital = false (the C# value 5/4 factor); connectivity does NOT change the distance | openciv3 | City.cs:972/981; exe address GUESS so D2a applies (final ruling 11-22, ends the A1-2 loop) | corruption,engine | 2026-11-22 |
| R-004 | Corruption: wonder gate uses real connectivity | The corruption-reducing wonder gate uses real trade-network connectivity (CityConnectedToCapital) | civ3.exe | FUN_004adf90 (CONFIRMED, re/corruption 1.4) | corruption,wonders | 2026-11-22 |
| R-005 | Corruption-reducing wonders stack | Each qualifying wonder reduces corruption (0.7 per wonder); small and large counted as the C# does | custom | D3 (owner 11-21) | corruption | 2026-11-22 |
| R-006 | Slot-01 save fixtures | The slot-01 turn-93 save constructs NO corruption-reducing wonder: real-save assertions are 0/0; a synthetic clone test covers 2/0 | custom | fixture fact verified 11-22 (T011/T015/T016) | tests,corruption | 2026-11-22 |
| R-007 | City quantities display | City quantities are shown as an icon multiplied by N | custom | D1 | ui,city | 2026-11-22 |
| R-008 | Unit-plate bar | City cycle YES, stack move and same-type stack move YES, disorder filter LIKELY, unit-cycle buttons NO | custom | D5 (owner 11-21) | ui,orders | 2026-11-22 |
| R-009 | Governor | Governor tabs are PARITY; MVP default citizen management is FOOD and growth | custom | D6 | city,growth | 2026-11-22 |
| R-010 | Fog of war | A cheat AND a satellite-style wonder abolish fog entirely and persistently (whole map visible, nothing hidden); per player; the AI gets it only from its own wonder | custom | D7 | visibility,wonders | 2026-11-22 |
| R-011 | Difficulty and AI | Three difficulty levels (easy/medium/hard), no aggression levels, a FAIR AI (sees only what its player sees; no hidden bonuses) | custom | D8 | ai,newgame | 2026-11-22 |
| R-012 | MVP map generator | Port OpenCiv3's generator logic; settings fixed mid-range, about 3 continents; the C# repo is the owner's own fork so translation is allowed; civ3.exe's full generator comes after MVP | openciv3 | D9 (owner); MapGenerator.cs, HeightMap.cs, MapGenNoise.cs | mapgen | 2026-11-22 |
| R-013 | Espionage | Five missions; rare (1 per 10 turns per player, needs Intelligence Agency); flat odds; failure loses the spy and -2 attitude, never war; target's Intelligence Agency lowers the chance (default 25 points) | custom | D11 (owner 11-22) | espionage | 2026-11-22 |
| R-014 | Advisors: previously seen units | Visible-only counts ship; previously-seen-unit counting is a documented gap (no persisted observation identities) | custom | ADV-3a FRD rev 2 ruling 11-22 | advisors | 2026-11-22 |
| R-015 | Foreign advisor base matrix | The base mood matrix (what the C# serves) cuts over without DIP-1; only treaty/trade extensions wait for DIP-1 | openciv3 | ADV-3a ruling 11-22; peace primitives | advisors,diplomacy | 2026-11-22 |
| R-016 | Clone nil versus empty | State.Clone must keep nil slices nil (reflect.DeepEqual distinguishes nil from empty) | custom | engineering rule, found 11-22 (cloneTechs) | engine,tests | 2026-11-22 |
| R-017 | Unsatisfiable reviewer demand | A reviewer demand that is provably unsatisfiable (data does not exist) is overridden with a recorded reason, never looped | custom | process rule 11-22 (T011/T015/T016, A2-1) | process | 2026-11-22 |
| R-018 | Research seats | civ3.exe Ghidra research runs on the headless runners (kilo/Beam), never on freebuff seats | custom | process rule 11-22 (solar stalled) | process,seats | 2026-11-22 |
| R-019 | Budgets | Effort token budgets are optional (unlimited); only metered backends get caps; Beam direct accounts are the only metered candidates | custom | owner 11-22 | agentflow | 2026-11-22 |
| R-020 | Worker order | Free runners first (big-pickle, muse, Beam, kilo Beam); grok LAST and 2 slots (work account, 63% waste measured) | custom | owner 11-22 | agentflow | 2026-11-22 |
| R-021 | agentflow autonomy | Apart from FRDs, owner decisions and limits, agentflow runs itself; snags climb the ladder before any person is asked | custom | owner 11-22 (AF-19, AF-21) | agentflow | 2026-11-22 |
| R-022 | Owner override | The owner may overrule any ruling, oracle test or research finding at any time by saying so; it becomes a ruling and the departure is recorded as an allow-list entry citing the decision (D4) | custom | owner 11-22 | process | 2026-11-22 |
| R-023 | No ruling exists | Follow the plan text; if the plan is silent, choose the simplest behaviour that satisfies it, state the choice in the report, and do not block on research (both openciv3 and civ3.exe are incomplete) | custom | owner 11-22 | process | 2026-11-22 |
