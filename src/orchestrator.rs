//! Which agent CLI drives a review, and which one takes over when it cannot.
//!
//! The orchestrator is the session that runs the review skill: it fans the
//! panel out, synthesizes, posts, and approves. It is one dash-p harness, and
//! dash-p exits 10 when that harness fails -- an outage, a usage limit, an
//! is_error turn. Nothing about the PR caused that, so the review is worth
//! retrying under a different provider. That retry is the fallback.
//!
//! Two backends can orchestrate: claude and codex. Both find the review
//! skills in the Agent Skills layout and both take an explicit skill
//! invocation, which is what an unattended prompt has to be. The two differ
//! in what dash-p forwards to them, and every difference is spelled out here
//! rather than discovered at the summary: codex gets no session flag, no
//! system prompt, and no budget cap.

/// Probed in this order for the default fallback, so codex is the stand-in
/// for claude and claude the stand-in for codex.
pub const BACKENDS: [&str; 2] = ["codex", "claude"];

/// One orchestrator as asked for on the command line: a backend, and
/// optionally the model to pin it to.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Orchestrator {
    pub backend: String,
    pub model: Option<String>,
}

impl Orchestrator {
    pub fn claude() -> Orchestrator {
        Orchestrator { backend: "claude".into(), model: None }
    }

    /// `backend` or `backend:model`.
    pub fn parse(raw: &str) -> Result<Orchestrator, String> {
        let (backend, model) = match raw.split_once(':') {
            Some((b, m)) => (b, Some(m)),
            None => (raw, None),
        };
        if !BACKENDS.contains(&backend) {
            return Err(format!(
                "error: unknown orchestrator \"{backend}\" (expected one of: {})",
                BACKENDS.join(", ")
            ));
        }
        if model.is_some_and(str::is_empty) {
            return Err(format!("error: orchestrator \"{raw}\" names no model after the colon"));
        }
        Ok(Orchestrator {
            backend: backend.to_string(),
            model: model.map(str::to_string),
        })
    }

    /// The spec as typed: `claude`, or `codex:gpt-5.5`.
    pub fn label(&self) -> String {
        match &self.model {
            Some(m) => format!("{}:{m}", self.backend),
            None => self.backend.clone(),
        }
    }

    /// The CLI dash-p will drive, which has to be on PATH.
    pub fn cli(&self) -> &str {
        &self.backend
    }

    /// dash-p forwards `--session-id` and `--resume` to claude alone. A codex
    /// review runs in a thread codex allocates, and nothing can ask for it
    /// again: every codex review is a fresh review.
    pub fn supports_sessions(&self) -> bool {
        self.backend == "claude"
    }

    /// `--max-budget-usd` is a claude flag, and dash-p forwards it to claude
    /// alone. A codex review runs uncapped.
    pub fn supports_budget(&self) -> bool {
        self.backend == "claude"
    }

    /// `--append-system-prompt` reaches claude alone. Anything that has to
    /// reach a codex reviewer rides in the prompt itself.
    pub fn supports_system_prompt(&self) -> bool {
        self.backend == "claude"
    }

    /// Whether this backend finds a skill in the directory a run stages and
    /// hands over with `--add-dir`. The staged tree is a `.claude/skills`
    /// directory, which claude reads and codex does not: codex resolves
    /// skills from its own roots (`$CODEX_HOME/skills` and
    /// `~/.agents/skills`) and from its project root, never from a
    /// directory added at run time. Verified against both CLIs rather than
    /// assumed -- a review whose skill never triggered is an expensive way
    /// to find out.
    pub fn discovers_staged_skills(&self) -> bool {
        self.backend == "claude"
    }

    /// Where this backend does look for the review skills, for the message
    /// that tells an operator how to make them reachable.
    pub fn skills_home(&self) -> &'static str {
        if self.backend == "codex" { "~/.agents/skills" } else { "~/.claude/skills" }
    }

    /// How this backend is told to run a skill by name. Claude Code takes a
    /// slash command; codex reserves `/` for its own commands and takes
    /// `$name` as the explicit skill invocation. Both are named rather than
    /// described in prose: an unattended one-shot has no human to correct a
    /// prompt that failed to trigger the skill.
    pub fn skill_prompt(&self, skill: &str, pr: u64) -> String {
        if self.backend == "codex" {
            format!("${skill} {pr}")
        } else {
            format!("/{skill} {pr}")
        }
    }

    /// The command that reopens a finished review interactively.
    pub fn reopen_command(&self) -> &'static str {
        if self.backend == "codex" { "codex resume <SESSION>" } else { "claude --resume <SESSION>" }
    }
}

/// What the operator asked for as the stand-in, before PATH has been looked
/// at. `Auto` becomes a backend at run start, once it is known what is
/// installed; a parse-time decision would refuse `--help` on a box with no
/// codex, and would make the unit tests depend on the developer's PATH.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Fallback {
    Auto,
    None,
    Spec(Orchestrator),
}

impl Fallback {
    /// `none`, `auto`, or an orchestrator spec.
    pub fn parse(raw: &str) -> Result<Fallback, String> {
        match raw {
            "none" | "off" => Ok(Fallback::None),
            "auto" => Ok(Fallback::Auto),
            spec => Orchestrator::parse(spec)
                .map(Fallback::Spec)
                .map_err(|e| e.replace("unknown orchestrator", "unknown fallback")),
        }
    }

    /// The words the startup line uses for the stand-in. Naming *why* there
    /// is none is the point: a bare "none" reads as a choice, when it may be
    /// a CLI that is simply not installed -- and the operator who wanted a
    /// fallback would find out only from the pass that needed one.
    pub fn describe(&self, primary: &Orchestrator, resolved: Option<&Orchestrator>) -> String {
        match (self, resolved) {
            (_, Some(f)) => f.label(),
            (Fallback::None, None) => "none".into(),
            (_, None) => format!("none ({} is not installed)", other_than(primary)),
        }
    }

    /// The fallback this run will actually use. `installed` answers whether a
    /// CLI is on PATH, injected so the choice is unit-testable.
    ///
    /// Auto picks the first backend that is not the primary's and is
    /// installed. An explicit spec is returned as-is: whether it is installed
    /// is checked by the caller, which can refuse the run with a message --
    /// an operator who named a fallback wants to know it is missing, where
    /// one who named nothing wants the run to go ahead.
    pub fn resolve(&self, primary: &Orchestrator, installed: &dyn Fn(&str) -> bool) -> Option<Orchestrator> {
        match self {
            Fallback::None => None,
            Fallback::Spec(o) => Some(o.clone()),
            Fallback::Auto => BACKENDS
                .iter()
                .find(|b| **b != primary.backend && installed(b))
                .map(|b| Orchestrator { backend: b.to_string(), model: None }),
        }
    }
}

/// The backend an automatic fallback would reach for. Naming the CLI is what
/// makes "there is no fallback" actionable.
pub fn other_than(primary: &Orchestrator) -> &'static str {
    BACKENDS.iter().copied().find(|b| *b != primary.backend).unwrap_or("codex")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn specs_parse_to_backend_and_model() {
        assert_eq!(Orchestrator::parse("claude").unwrap(), Orchestrator::claude());
        assert_eq!(
            Orchestrator::parse("codex:gpt-5.5").unwrap(),
            Orchestrator { backend: "codex".into(), model: Some("gpt-5.5".into()) }
        );
        assert_eq!(Orchestrator::parse("codex:gpt-5.5").unwrap().label(), "codex:gpt-5.5");
        assert_eq!(Orchestrator::claude().label(), "claude");
    }

    #[test]
    fn a_backend_that_cannot_orchestrate_is_refused() {
        // opencode reviews as a panelist, but nothing here can hand it a
        // skill by name.
        let e = Orchestrator::parse("opencode").unwrap_err();
        assert!(e.contains("unknown orchestrator \"opencode\""), "got {e}");
        assert!(e.contains("codex, claude"), "lists the choices: {e}");
        let e = Orchestrator::parse("codex:").unwrap_err();
        assert!(e.contains("names no model"), "got {e}");
    }

    #[test]
    fn each_backend_is_told_about_a_skill_its_own_way() {
        assert_eq!(Orchestrator::claude().skill_prompt("auto-review", 9), "/auto-review 9");
        let codex = Orchestrator::parse("codex").unwrap();
        assert_eq!(codex.skill_prompt("auto-review", 9), "$auto-review 9");
        assert_eq!(codex.reopen_command(), "codex resume <SESSION>");
        assert_eq!(Orchestrator::claude().reopen_command(), "claude --resume <SESSION>");
    }

    #[test]
    fn what_dashp_forwards_is_stated_per_backend() {
        let claude = Orchestrator::claude();
        assert!(claude.supports_sessions() && claude.supports_budget() && claude.supports_system_prompt());
        let codex = Orchestrator::parse("codex").unwrap();
        assert!(!codex.supports_sessions() && !codex.supports_budget() && !codex.supports_system_prompt());
    }

    #[test]
    fn only_claude_finds_the_skills_a_run_stages() {
        // The staged tree is a .claude/skills directory. Handing it to
        // codex would widen its writable set and buy nothing.
        assert!(Orchestrator::claude().discovers_staged_skills());
        let codex = Orchestrator::parse("codex").unwrap();
        assert!(!codex.discovers_staged_skills());
        assert_eq!(codex.skills_home(), "~/.agents/skills");
        assert_eq!(Orchestrator::claude().skills_home(), "~/.claude/skills");
    }

    #[test]
    fn fallback_words() {
        assert_eq!(Fallback::parse("none").unwrap(), Fallback::None);
        assert_eq!(Fallback::parse("off").unwrap(), Fallback::None);
        assert_eq!(Fallback::parse("auto").unwrap(), Fallback::Auto);
        assert_eq!(
            Fallback::parse("codex:gpt-5.5").unwrap(),
            Fallback::Spec(Orchestrator::parse("codex:gpt-5.5").unwrap())
        );
        let e = Fallback::parse("gemini").unwrap_err();
        assert!(e.contains("unknown fallback \"gemini\""), "got {e}");
    }

    #[test]
    fn auto_picks_the_other_installed_backend() {
        let all = |_: &str| true;
        let claude = Orchestrator::claude();
        assert_eq!(Fallback::Auto.resolve(&claude, &all).unwrap().backend, "codex");
        let codex = Orchestrator::parse("codex").unwrap();
        assert_eq!(Fallback::Auto.resolve(&codex, &all).unwrap().backend, "claude");
        // The model is never carried over: the stand-in runs on its own default.
        let pinned = Orchestrator::parse("claude:opus-4.8").unwrap();
        assert_eq!(Fallback::Auto.resolve(&pinned, &all), Some(Orchestrator::parse("codex").unwrap()));
    }

    #[test]
    fn the_startup_line_says_why_there_is_no_fallback() {
        let claude = Orchestrator::claude();
        let codex = Orchestrator::parse("codex").unwrap();
        assert_eq!(Fallback::Auto.describe(&claude, Some(&codex)), "codex");
        // Turned off on purpose reads as a choice.
        assert_eq!(Fallback::None.describe(&claude, None), "none");
        // Not turned off, but nothing to fall back to: say which CLI is
        // missing, because installing it is the fix.
        assert_eq!(Fallback::Auto.describe(&claude, None), "none (codex is not installed)");
        assert_eq!(Fallback::Auto.describe(&codex, None), "none (claude is not installed)");
    }

    #[test]
    fn auto_is_nothing_when_nothing_else_is_installed() {
        let only_claude = |cli: &str| cli == "claude";
        assert_eq!(Fallback::Auto.resolve(&Orchestrator::claude(), &only_claude), None);
        // A named fallback is handed back whatever PATH says; the caller
        // refuses the run if it is missing.
        let named = Fallback::Spec(Orchestrator::parse("codex").unwrap());
        assert_eq!(named.resolve(&Orchestrator::claude(), &only_claude).unwrap().backend, "codex");
        assert_eq!(Fallback::None.resolve(&Orchestrator::claude(), &|_| true), None);
    }
}
