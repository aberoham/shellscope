# RACONTEUR: A Knowledgeable, Insightful, and Portable LLM-Powered Shell Command Explainer

## Source

- Primary source: https://www.ndss-symposium.org/ndss-paper/raconteur-a-knowledgeable-insightful-and-portable-llm-powered-shell-command-explainer/
- arXiv preprint (v1, 2024-09-03): https://arxiv.org/abs/2409.02074
- arXiv HTML (used for technical extraction): https://arxiv.org/html/2409.02074v1
- Project page: https://raconteur-ndss.github.io/
- Camera-ready PDF: https://www.ndss-symposium.org/wp-content/uploads/2025-s798-paper.pdf
- Authors: Jiangyi Deng, Xinfeng Li, Yanjiao Chen, Yijie Bai, Wenyuan Xu (Zhejiang
  University); Haiqin Weng, Yan Liu, Tao Wei (Ant Group).
- Venue: NDSS Symposium. The arXiv abstract and the project page list NDSS
  2025; the NDSS paper page accessed for this note shows NDSS 2026. The
  ambiguity is treated as an open question below; the work itself was first
  posted in September 2024.
- Reviewed: 2026-04-25.

## What It Is

RACONTEUR is a research prototype that takes a shell command as input and
returns a structured, natural-language explanation of what the command does
together with a mapping to MITRE ATT&CK tactics and techniques. The system is
designed for security analysts inspecting suspicious commands on Unix-like
shells and on PowerShell. It is built around a fine-tuned open-source language
model and two retrieval components, and is presented as a portable, locally
deployable alternative to general-purpose LLM APIs for this task.

## Relevant Capabilities

- Step-by-step explanation of a single shell command or a compound command
  (multiple utilities combined with pipes, redirections, and command
  separators), covering utilities, options, and parameters.
- Inference of intent: in addition to "what" the command does, the explainer
  attempts to characterize "why" it does it.
- A behavior summary that can flag potential malicious attempts within the
  command.
- Translation of the natural-language behavior description into MITRE ATT&CK
  technique and tactic labels via an embedding-based matcher.
- Documentation-augmented explanation for previously unseen commands, by
  retrieving and conditioning on snippets from external documentation.
- Bilingual operation: the underlying chat model is Chinese-English, and the
  evaluation set includes both languages.

## Requirements and Operating Model

- Inputs: the raw command string. The paper does not describe consuming
  surrounding session context such as working directory, parent process,
  preceding commands, environment variables, or terminal output.
- Granularity: single shell command or compound one-liner. The authors
  explicitly note that extending the system to multi-command shell sessions
  (sequences of commands) is left as future work.
- Base model: ChatGLM2-6B, an open-source 6-billion-parameter bilingual
  (Chinese/English) chat model, fully fine-tuned for the behavior explainer.
  Reported training cost is four NVIDIA A100 80 GB GPUs for four days at batch
  size 16, learning rate 1e-4, 42,000 steps, with a maximum sequence length of
  1,024 tokens, for a total of roughly 232 million tokens consumed.
- Behavior explainer training corpus: about 254,000 (prompt, response) pairs.
  Commands are sourced from Atomic Red Team and Metta (both ATT&CK-aligned
  malicious sets), a Metasploit-generated reverse-shell set, NL2Bash, and the
  Unix Shell and PowerShell subsets of The Stack, split 9:0.5:0.5 for
  train/val/test. Responses are constructed with a knowledge-infused prompt
  that combines command documentation, meta-information, and ATT&CK labels and
  are then organized by GPT-3.5-Turbo.
- Intent identifier (BD2Vec, "behavior-description-to-vector"): a Text2Vec
  embedding model fine-tuned on (behavior, technique description, label)
  triples derived from MITRE ATT&CK. The reported best variant is a
  fine-tuned E5-large with AUC 0.981 on the matching task. Mapping is
  performed by ranking technique descriptions by embedding similarity to the
  generated behavior text; tactic identification averages similarity over
  techniques per tactic and selects the top tactic.
- Documentation retriever (CD2Vec, "command-and-doc-to-vector"): an E5-large
  encoder (~330M parameters) adapted with LoRA on roughly 952,000 (command,
  documentation, label) triples extracted from the Linux man pages of 1,662
  Bash utilities, with embedding dimension 1,024.
- Deployment shape: presented as a local, on-premise prototype using
  open-source models, framed as preferable to sending potentially proprietary
  commands to a cloud LLM API.

## Outputs and Integration Points

- Per-command explanation block consisting of a step-by-step breakdown of
  utilities, flags, and arguments, followed by an overall behavior summary.
- An attached MITRE ATT&CK label of the form "Tactic: <tactic>, Technique:
  <technique>." Mapping uses 14 ATT&CK tactics and roughly 200 techniques.
- An optional retrieval-augmented section grounded in fetched documentation
  for commands the model has not seen during training.
- The artifact released alongside the paper is the curated dataset (linked
  from the project page via a Google Drive download). At the time of this
  review the project page surfaces the dataset link but the "Code" buttons
  are placeholder anchors, and no public source code or model checkpoint
  repository is referenced. This is treated as an open question below.

## Limitations and Risks

The following are limitations stated by the authors or visible in the
methodology:

- Granularity ceiling: the system is trained and evaluated on individual
  commands and compound one-liners. Extending it to multi-command sessions or
  shell scripts is acknowledged as future work.
- Obfuscation: the paper notes the absence of a benchmark for obfuscated
  commands and defers a comprehensive evaluation. The system can decode and
  explain Base64-encoded payloads in some cases, but performance on
  sophisticated obfuscation is not characterized.
- Base-model size: ChatGLM2-6B is small relative to GPT-3.5/4 and the authors
  acknowledge that a 6B model "may not" outperform much larger models in all
  conditions; their published wins rely on domain-specific fine-tuning.
- Hallucination: hallucination of nonexistent flags or behaviors is the
  motivating concern for the knowledge-infusion design; the paper does not
  publish a stand-alone hallucination benchmark.
- Context narrowness: only the command string is consumed. There is no
  mechanism described for incorporating session timing, output, the operator's
  prior actions, host facts, or whether the command actually succeeded.
- Threat model assumes the analyst can decipher obfuscated commands once the
  obfuscation is recognized; the system aids interpretation, it does not
  deobfuscate end-to-end.
- No latency, throughput, inference-cost, or prompt-injection-resistance
  numbers are reported.

## Evaluation Summary

- Behavior explainer baselines: GPT-3.5-Turbo, GPT-4, and the unfine-tuned
  ChatGLM2-6B base.
- Intent identifier baselines: five Text2Vec variants including
  Sentence-T5large, GTR-T5XL, SGPT, E5-large, and the fine-tuned E5-large
  used by RACONTEUR.
- Documentation retriever baselines: Sentence-T5large, GTR-T5XL, SGPT, and
  off-the-shelf E5-large.
- Reference metrics for explanation quality: ROUGE-1/2/L, BLEU-4, METEOR,
  CIDEr; end-to-end the authors also report precision, recall, and accuracy
  for the malicious/benign classification implied by the explanation.
- Headline numbers reported by the authors:
  - On malicious commands, RACONTEUR scores about 68.9 versus about 45.5 for
    GPT-4 on a ROUGE-family aggregate (the paper's tabular comparison).
  - On benign commands, about 69.3 versus about 51.8 for GPT-4 on the same
    aggregate.
  - End-to-end command classification accuracy of about 81.8% versus about
    70.8% for GPT-4.
  - Top-1 technique-identification accuracy on a balanced test set of about
    56.0% versus about 28.1% for GPT-4.
  - Documentation-retrieval AUC of 0.981 for the fine-tuned matcher versus a
    baseline average of about 0.858.
- Human study: 52 computer-science students (mixed proficiency) rated 40
  command explanations on a 1-5 scale for comprehensiveness, intent clarity,
  and malicious/benign judgment, plus pairwise preference questions against
  baselines. The authors report that participants preferred RACONTEUR for the
  detail and explicit warning behavior of its output.

## Open Questions

- The arXiv abstract and project page state NDSS 2025; the NDSS paper page
  accessed during review labels the work NDSS 2026. Which is the canonical
  publication year for citation purposes?
- The project page exposes the curated dataset via a Google Drive link but
  the "Code" buttons appear to be placeholders. Has source code, the
  fine-tuned ChatGLM2-6B checkpoint, the BD2Vec model, or the CD2Vec model
  been released anywhere reproducible?
- How robust is the ATT&CK technique mapping to commands whose intent is
  ambiguous in isolation but clear in session context (for example, a
  benign-looking `tar` whose meaning depends on the preceding `find`)?
- How does the system behave on commands that reference non-stdlib utilities
  added through the documentation retriever, and what are the failure modes
  when the retrieved documentation is wrong or stale?
- How well does the ATT&CK mapping degrade on out-of-distribution shells
  (BSD, busybox, embedded shells, container ENTRYPOINTs, init scripts), none
  of which appear to be evaluated explicitly?
- The published evaluation does not include adversarial inputs designed to
  manipulate the explainer (prompt injection embedded in command arguments,
  retrieved man pages, or filenames). What is the empirical prompt-injection
  surface, and what is the practical throughput envelope of the fine-tuned 6B
  model when wired into a busy audit stream?
