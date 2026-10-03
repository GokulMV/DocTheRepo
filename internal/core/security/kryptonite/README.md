# kryptonite (vendored)

The attack modules and references in this directory come from
[levitasOrg/kryptonite](https://github.com/levitasOrg/kryptonite) at commit `229a4f778870621ccbd3203a51ff4347efd2b0f1`, under the MIT
licence in [LICENSE](LICENSE). They are embedded unchanged; the Hub's Security scans use them as the
playbooks for each attack dimension (see docs/security-scans.md for how the phases map onto the Hub).

To update: copy `modules/*.md` (without `_TEMPLATE.md`) and `references/{severity,finding-schema,agent-prompts}.md`
from a newer commit and change the commit above.
