import z from "@deepseek-ai/schemastery";
import { defineTool } from "@deepseek-ai/dsh-tools";
import { assertSubagentMaxDepth, settleRun } from "@deepseek-ai/dsh-subagent";

/**
 * dsh-subagent-max — host half (self-developed).
 *
 * Registers `subagent_with_model`, a thin wrapper over the native subagent
 * service that adds a per-call `model` / `provider` override. All execution
 * still goes through `ctx.subagents` and the native jobs service; nothing is
 * reimplemented.
 */
export const name = "dsh-subagent-max";
export const inject = ["tools", "subagents"];

export const Config = z.object({
  subagentProvider: z.string().default("spawn"),
  toolName: z.string().default("subagent_with_model"),
  backgroundMode: z.union(["one-shot", "continuable"]).default("one-shot"),
  maxDepth: z.natural().default(3),
});

function outputText(blocks) {
  return (blocks ?? [])
    .filter((b) => b !== null && typeof b === "object" && b.type === "text" && typeof b.text === "string")
    .map((b) => b.text)
    .join("");
}

function stopMessage(result) {
  switch (result.stopReason) {
    case "aborted": return "subagent run was cancelled";
    case "error": return "subagent run failed";
    case "max-tokens": return "subagent run hit its token limit before finishing";
    case "refusal": return "subagent declined the task";
    default: return "subagent run ended abnormally (" + String(result.stopReason) + ")";
  }
}

async function settleStart(startPromise, signal) {
  try {
    return await settleRun(await startPromise);
  } catch (error) {
    return signal.aborted ? { status: "killed" } : { status: "failed", detail: String(error) };
  }
}

export function apply(ctx, config) {
  assertSubagentMaxDepth(config.maxDepth);
  const continuable = (config.backgroundMode ?? "one-shot") === "continuable";
  const toolName = config.toolName ?? "subagent_with_model";
  let disposeTool;

  function mount(provider) {
    if (continuable && typeof provider.prepareContinuable !== "function") {
      throw new Error("dsh-subagent-max: provider " + provider.name + " does not support backgroundMode: continuable");
    }
    disposeTool = ctx.tools.register(defineTool({
      name: toolName,
      description:
        "Delegate a task to a subagent and explicitly choose its model. The child runs on the same native subagent engine as the regular subagent tool; model (and optionally provider) select that child's model for this one delegation.",
      parameters: {
        model: { type: "string", required: true, description: "The model id the child subagent must use (e.g. deepseek-v4-pro, deepseek-v4-flash, k3-256k)." },
        provider: { type: "string", description: "Optional LLM provider route for the child. Omit to inherit the parent's provider." },
        description: { type: "string", required: true, description: "A short (3-5 word) description of the delegated task." },
        prompt: { type: "string", required: true, description: "The complete, self-contained task for the subagent." },
        run_in_background: { type: "boolean", description: "Whether to run in the background and return an id to collect later." },
      },
      output: {
        schema: { type: "json" },
        render: (_args, value) => {
          const text =
            value?.kind === "continuable" ? "started subagent " + value.subagentId :
            value?.kind === "background" ? "started background subagent task " + value.jobId :
            outputText(value?.output ?? []);
          return [{ type: "text", text }];
        },
      },
      isConcurrencySafe: () => true,
      async execute(args, exec) {
        const parent = exec.agent;
        if (!parent) throw new Error("subagent tool requires a calling agent");

        const agentOptions = {
          ...(args.provider !== undefined ? { provider: args.provider } : {}),
          ...(args.model !== undefined ? { model: args.model } : {}),
        };
        const request = {
          prompt: [{ type: "text", text: args.prompt }],
          parent,
          agentOptions,
          maxDepth: config.maxDepth,
        };
        const runInBackground = args.run_in_background ?? continuable;

        if (runInBackground) {
          if (continuable) {
            const { childId } = await ctx.subagents.startContinuable({
              provider: config.subagentProvider,
              label: args.description,
              request,
              signal: exec.signal,
            });
            return { kind: "continuable", subagentId: childId };
          }
          const jobs = ctx.get("jobs");
          if (!jobs) throw new Error("background jobs unavailable");
          return {
            kind: "background",
            jobId: jobs.start({
              kind: "subagent",
              label: args.description,
              owner: parent,
              run: () => {
                const controller = new AbortController();
                return {
                  cancel: (reason) => controller.abort(reason ?? "background subagent task killed"),
                  done: settleStart(
                    ctx.subagents.start(config.subagentProvider, { ...request, label: args.description, signal: controller.signal }),
                    controller.signal,
                  ),
                };
              },
            }),
          };
        }

        const run = await ctx.subagents.start(config.subagentProvider, { ...request, label: args.description, signal: exec.signal });
        try {
          const result = await run.result;
          if (result.stopReason !== "completed") {
            const partial = outputText(result.output ?? []);
            throw new Error(partial ? stopMessage(result) + "\nPartial output before the run ended:\n" + partial : stopMessage(result));
          }
          return { kind: "foreground", runId: run.id, output: result.output };
        } finally {
          await run.dispose();
        }
      },
    }));
  }

  ctx.on("subagent/provider-added", (provider) => {
    if (provider.name === config.subagentProvider && disposeTool === undefined) mount(provider);
  });
  ctx.on("subagent/provider-removed", (name) => {
    if (name === config.subagentProvider && disposeTool !== undefined) {
      disposeTool();
      disposeTool = undefined;
    }
  });

  const present = ctx.subagents.getProvider(config.subagentProvider);
  if (present !== undefined) mount(present);
  else ctx.logger.info("subagent provider " + config.subagentProvider + " not registered yet; tool " + toolName + " will register when it appears");
}
