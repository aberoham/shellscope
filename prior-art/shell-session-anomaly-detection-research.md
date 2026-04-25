# Shell Session Anomaly Detection: Research Prior Art

## Sources

### Anchor paper 1 — DistilBERT shell anomaly detection

- Primary source: https://arxiv.org/abs/2310.13247 (HTML at
  https://arxiv.org/html/2310.13247)
- Index entry: https://paperswithcode.com/paper/anomaly-detection-of-command-shell-sessions
  (redirects to a Hugging Face Papers mirror at
  https://huggingface.co/papers/2310.13247)
- Title: "Anomaly Detection of Command Shell Sessions based on DistilBERT:
  Unsupervised and Supervised Approaches"
- Authors: Zefang Liu, John Buford (JPMorgan Chase)
- Venue: arXiv preprint, submitted 2023-10-20. The arXiv listing does not
  record a peer-reviewed venue. cs.CL and cs.CR cross-listing.
- Reviewed: 2026-04-25

### Anchor paper 2 — PASTRAL

- Primary source (PDF):
  https://assets.amazon.science/0d/30/4db26e484e76a476ccd8ab35a53f/pastral-workshop-camera-ready-v1.pdf
- Publication page:
  https://www.amazon.science/publications/pastral-privacy-aware-ast-and-transformer-based-anomalous-command-line-detection
- Title: "PASTRAL: Privacy-aware AST and TRansformer-based Anomalous
  command-Line detection"
- Authors: Xiayan Ji, Ecenaz Erdemir, Kyuhong Park, Bhavna Soman, Yi Fan
  (Amazon Web Services, New York)
- Venue: NeurIPS 2025 Workshop on Continual and Compatible Foundation Model
  Updates (workshop camera-ready PDF). Not located on arXiv as of the review
  date.
- Reviewed: 2026-04-25

### Brief related-work sources cited for context

- Schonlau et al. "Computer intrusion: Detecting masquerades", Statistical
  Science, 2001 — the SEA dataset of Unix commands from 50 users.
- Greenberg, "Using Unix: Collected traces of 168 users", University of
  Calgary technical report, 1988.
- Lane and Brodley, the Purdue (PU) command history dataset, 1997.
- Lin et al., "NL2Bash", LREC 2018 — paired natural-language and bash command
  corpus.
- CrowdStrike blog series referenced by Liu and Buford — BERT embeddings plus
  PyOD ensemble for command-line anomaly detection (2022, two parts).
- Huang et al., "CmdCaliper", EMNLP 2024 — semantic command-line embedding
  model and dataset.
- Lin, Guo, Chen, "Intrusion detection at scale with the assistance of a
  command-line language model" (2024) — referenced by PASTRAL as the IDS-LLM
  baseline.

## What This Cluster Is

Two recent papers apply transformer language models to anomaly detection in
Unix-style command shells. They sit on top of an older masquerade-detection
literature whose canonical datasets are command-only sequences from a small
fixed user pool. The new work moves to enterprise-scale or multi-user
production data, treats commands as text for a pretrained encoder, and adds
either ensemble outlier detection or a generative reconstruction model on top.
PASTRAL further introduces an explicit privacy mechanism so that raw command
text never leaves the originating machine.

## Per-Paper Analysis

### DistilBERT shell anomaly detection (Liu and Buford, 2023)

#### Problem framing

Per-session anomaly scoring for interactive Unix shell sessions, intended for
analyst triage. Authors explicitly separate this from masquerade detection
("is this the right user?") and frame their target as suspicious or
exploitable command patterns regardless of user identity. The model is
positioned as a complement to existing rule-based detection (e.g., MITRE
ATT&CK signatures), surfacing outliers that rules miss.

#### Inputs

90 days of Unix keystroke captures from a production enterprise estate, more
than 15,000 users, around 3 million activity records, ~2.4 million non-empty
interactive sessions. Heuristic preprocessing extracts shell prompts,
separates user input from command output, concatenates wrapped multi-line
commands, drops editor buffers, masks numbers and special tokens, and removes
cyclic loop output. After preprocessing, ~1.15 million unique sessions
remain. Argument-aware (commands kept with arguments; numerics masked).
Subshell sessions (HDFS, Spark, SQL, Python) are split out and processed in a
parallel pipeline; subshell results are out of scope of the paper.

#### Method

DistilBERT encoder trained from scratch on the cleaned shell corpus (authors
argue NL-pretrained models do not transfer well to shell text). WordPiece
tokenizer, vocabulary 30,000, cased. Training objective is masked language
modeling on whole sessions. Per-session embedding is the last hidden state.

Unsupervised detection: ensemble of four PyOD outlier detectors over the
embeddings — PCA, isolation forest, COPOD, autoencoder. Per-detector scores
are normalized and averaged. This ensemble follows a CrowdStrike blog-series
framework cited by the paper.

Supervised detection: SetFit (contrastive Siamese few-shot fine-tuning) on
the same encoder, compared against vanilla DistilBERT fine-tune and logistic
regression on frozen embeddings. Labels are not human annotations; sessions
are weakly labeled by counting unique "suspicious" keywords drawn from an
Uptycs blog mapping attacker tools to ATT&CK techniques (>=3 unique keywords
= abnormal, 0 = normal, in-between dropped as abstained). Whole-session
embedding throughout; no sliding window.

#### Datasets used

Proprietary 90-day production capture described above; not released. SEA
(Schonlau), Greenberg, Purdue (PU), and NL2Bash are discussed in related
work but not used for evaluation.

#### Evaluation

Unsupervised: no ground truth, evaluation is qualitative — score
distribution, score vs token/line count, per-command average score, and
three illustrative high-score sessions annotated with ATT&CK techniques
(remote code exec / data exfiltration / disk wipe).

Supervised: precision, recall, F1 against the keyword-derived labels. Best
configuration is SetFit at 2048 samples per class. Numeric scores appear in
Table 3 of the paper; not transcribed here. No ROC-AUC, no externally
calibrated FPR, no comparison against another shell anomaly system. The
implicit baseline is rule-based scripts already in operations use; the paper
claims outliers found that those scripts miss but does not quantify base
rates.

#### Limitations the authors raise

No labeled ground-truth corpus at this scale; the supervised labels are
keyword-derived and inherit the keyword list's blind spots. Anomaly score
does not equal "suspicious" — long or unusual benign sessions also score
high. Tokenizer is generic WordPiece; a shell-specific tokenizer is future
work. Subshell handling is described but not evaluated.

#### Code, data, reproducibility

No public code or model release referenced on the arXiv page. Dataset is
proprietary enterprise capture and not redistributable.

### PASTRAL (Ji, Erdemir, Park, Soman, Fan, 2025)

#### Problem framing

Detect suspicious command-lines in a multi-tenant setting while preventing
the service provider (SP) from seeing raw command text. Authors motivate
anomaly detection because suspicious activity is too rare and too varied for
supervised classifiers or static signatures. Threat model is an
honest-but-curious SP that runs detection but should not be able to infer
sensitive content (API tokens, file paths, credentials) from what users
send. Privacy concern is about the SP and downstream investigators, not a
network adversary.

#### Inputs

Per-line CodeBERT embedding of each command-line, plus a per-line AST
embedding extracted via Tree-sitter using absolute root-to-token paths.
Sessions pool across lines: average pooling for CodeBERT (content), max
pooling for AST (context). Maximum of L lines per session (L not explicitly
stated in the body we read). Argument-aware; preprocessing decodes Base64
payloads, re-parses nested language fragments (e.g., Python inside shell)
with the appropriate Tree-sitter grammar, and replaces URLs and IPs with
sentinel tokens.

#### Method

Two embedding streams (CodeBERT content, AST context), each L2-normalized so
sensitivity is bounded by 2. Privacy mechanism is Gaussian differential
privacy noise added to each embedding stream on the user side; advanced
composition over the two queries gives the overall (epsilon, delta)
guarantee in Theorem 2.1, and post-processing immunity carries it through
the SP-side detector.

Detector is a conditional variational autoencoder: encoder takes the
concatenated content+context embedding; latent is conditioned on the AST
context; decoder reconstructs the content embedding. Anomaly score is the
squared reconstruction error normalized by dimension. Architecture uses a
downsampling ResNet block with residual connections, attention, group
normalization, and SiLU; latent sampled via the reparameterization trick.
Training objective is the standard CVAE ELBO (KL plus reconstruction),
trained only on benign data.

#### Datasets

Two in-house and two public sources. In-house "commands" collected from a
multi-year honeypot simulating SSH, web, and IoT — malicious samples
validated via VirusTotal or MITRE ATT&CK, benign drawn from attack-free
sessions. In-house "scripts" — longer command sequences, malicious from
VirusTotal and the honeypot, benign from clean VMs and GitHub repositories
with >=1000 stars. Public sets used for OOD testing only: "zenodo"
(Svabensky et al., shell commands from cybersecurity training) and "atomic"
(Red Canary's Atomic Red Team PowerShell). Training: ~73k benign scripts
and ~3.1k benign commands; test sets are balanced benign/malicious.

#### Evaluation

Metrics: FPR, recall, precision, F1, AUROC, with 95% CIs over five seeds.
Headline numbers (Table 1):

- scripts: AUROC 0.9885 vs IDS-LLM 0.9442; FPR 0.0049 vs 0.0420.
- cmd-lines: AUROC 1.0000 vs 0.9824; FPR 0.0001 vs 0.0128.
- zenodo (OOD): AUROC 1.0000 vs 0.9881; FPR 0.0000 vs 0.0450.
- atomic (OOD): AUROC 0.9948 vs 0.9589; FPR 0.0000 vs 0.0472.

Baseline is IDS-LLM (Lin et al., 2024), a language-model-only command-line
intrusion detector. Ablations show CodeBERT-only and AST-only each
underperform the combined model. Privacy-utility curve: at baseline
(epsilon=1.0, delta=1.0) AUROC is 0.99; at ~2x baseline noise
(epsilon=0.8, delta=0.7) AUROC drops about 2 points; at ~2.7x baseline noise
(epsilon=0.75, delta=0.5) it drops about 9 points. Authors also report
PASTRAL flags 99.3% of suspicious samples that 65 VirusTotal vendors miss,
after security-researcher labeling (Appendix F). LM-choice study: CodeBERT
matches StarCoder (3B) and Phi-3 (3.8B) accuracy at 24-30x fewer parameters
and 3-4x throughput.

#### Limitations the authors raise

Static analysis only; dynamically generated or staged runtime payloads are
out of scope and called out as future work. Privacy budget composes over two
queries per command, requiring careful noise calibration (handled via L2
normalization). Trade-off curve flattens past ~2x baseline noise; very
tight privacy budgets degrade detection meaningfully.

#### Code, data, reproducibility

No public code or trained model release referenced in the paper text. The
in-house datasets are proprietary; the public OOD sets are independently
available.

### Brief related-work context

The pre-transformer shell-anomaly literature is largely the "masquerader"
line: given commands attributed to user U, decide whether they came from U
or an imitator. Dominant benchmark is the SEA / Schonlau set (50 users,
truncated commands, no arguments), with Greenberg's 168-user trace and the
Purdue (Lane and Brodley) sets alongside. Methods include naive Bayes,
SVMs, HMMs, CNN/LSTM, and temporal CNNs. Liu and Buford argue this line is
poorly suited to the modern problem: (a) datasets are old and truncated,
(b) they lack arguments and subshell content, (c) the framing asks "is
this the right user?" rather than "is this an attack pattern?".

CrowdStrike's two-part 2022 blog series on BERT embeddings + PyOD
ensembles for command-line anomalies is the direct methodological ancestor
of the Liu and Buford pipeline. CmdCaliper (EMNLP 2024) trains a semantic
command-line embedding model and releases a dataset; PASTRAL cites it as
one of several LM-based command-line embedding choices.

## Cross-Paper Themes

- Transformer-on-shell-text is now the default representation. Both papers
  embed shell text with a pretrained encoder (DistilBERT trained from
  scratch in Liu and Buford; CodeBERT in PASTRAL) and layer a detector on
  top.
- Detector pattern differs: Liu and Buford use an ensemble of classical PyOD
  outlier detectors plus an optional supervised fine-tune; PASTRAL uses a
  single generative CVAE trained on benign data.
- Both retain command arguments and do explicit preprocessing (Base64
  decode, URL/IP normalization, numeric masking) — a clear break from the
  masquerader literature.
- Both pool to a fixed per-session vector; neither uses sliding windows.
- Ground truth is the persistent problem. Liu and Buford synthesize weak
  labels from a curated ATT&CK keyword list. PASTRAL builds labeled
  benign/malicious from honeypot + VirusTotal, plus public OOD sets.
  Neither uses Schonlau-style ground truth.
- Privacy: only PASTRAL formalizes a mechanism. Liu and Buford train
  centrally on raw enterprise capture.
- Both criticize older public datasets (SEA, Greenberg, PU, NL2Bash) as too
  small, too truncated, or too synthetic.
- Evaluation rigor differs sharply: PASTRAL reports AUROC/FPR/recall/
  precision/F1 with CIs and ablations; Liu and Buford report F1 against
  weak labels plus qualitative analysis.

## Open Questions

- Does the Liu and Buford DistilBERT corpus or model have any partial public
  release? The paper does not point at one, but JPMorgan Chase has released
  open-source assets for related projects in the past.
- What is the precise byte and token budget per session in either pipeline?
  PASTRAL caps to L lines per session but the paper text we read does not
  give L; Liu and Buford do not state a token cap explicitly either.
- For PASTRAL, what is the actual epsilon used in the headline Table 1
  numbers? The privacy-utility table shows curves around (epsilon=1.0,
  delta=1.0); whether Table 1 is at that operating point or pre-noise is not
  fully clear from the section we read.
- What happens to PASTRAL's guarantee under repeated queries on the same
  command across days? The composition argument covers two queries per
  command-line; query-volume budgeting at the user level is not addressed in
  the body we reviewed.
- Did either paper measure prompt-injection or evasion robustness (e.g.,
  shell sessions deliberately constructed to drive embeddings toward the
  benign cluster)? Neither paper appears to test this.
- Is there a more recent (post-2025) academic benchmark that supersedes both
  the JPMC enterprise corpus and PASTRAL's honeypot+VirusTotal split?
- How does CmdCaliper (EMNLP 2024) compare empirically to CodeBERT for
  command-line embedding? PASTRAL cites it but does not appear to benchmark
  against it directly.
