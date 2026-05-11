#!/usr/bin/env python3
"""Agent runtime for agents-as-code. Reads config.json, runs a PydanticAI agent, writes result.json."""

import json
import os
import signal
import subprocess
import sys
from pathlib import Path

from pydantic import BaseModel
from pydantic_ai import Agent, RunContext
from pydantic_ai.mcp import MCPServerStdio
from pydantic_ai.settings import ModelSettings


# -- Result schema (mirrors pkg/result/types.go) --

class ReviewComment(BaseModel):
    path: str
    line: int
    body: str


class Action(BaseModel):
    type: str
    body: str | None = None
    event: str | None = None
    comments: list[ReviewComment] | None = None
    add: list[str] | None = None
    remove: list[str] | None = None
    title: str | None = None
    head: str | None = None
    base: str | None = None
    context: str | None = None
    state: str | None = None
    description: str | None = None
    target_url: str | None = None
    message: str | None = None
    files: dict[str, str] | None = None


class Result(BaseModel):
    actions: list[Action] = []
    tokens_used: int = 0
    cost_usd: str = "0.00"


# -- Provider env var mapping --

PROVIDER_ENV_MAP = {
    "openai": "OPENAI_API_KEY",
    "anthropic": "ANTHROPIC_API_KEY",
    "gemini": "GEMINI_API_KEY",
    "google": "GEMINI_API_KEY",
    "groq": "GROQ_API_KEY",
    "mistral": "MISTRAL_API_KEY",
}



# -- Agent factory --

def create_agent(model: str, system_prompt: str) -> Agent[str, Result]:
    """Create an agent with workspace tools. Deps type is str (workspace path)."""
    agent = Agent(
        model,
        deps_type=str,
        output_type=Result,
        system_prompt=system_prompt,
    )

    @agent.tool
    def read_file(ctx: RunContext[str], path: str) -> str:
        """Read a file from the workspace. Returns the file content or an error message."""
        full_path = os.path.join(ctx.deps, path)
        if not os.path.isfile(full_path):
            return f"Error: {path} not found"
        with open(full_path) as f:
            return f.read()

    @agent.tool
    def write_file(ctx: RunContext[str], path: str, content: str) -> str:
        """Write content to a file in the workspace. Creates parent directories if needed."""
        full_path = os.path.join(ctx.deps, path)
        os.makedirs(os.path.dirname(full_path), exist_ok=True)
        with open(full_path, "w") as f:
            f.write(content)
        return f"Wrote {len(content)} bytes to {path}"

    @agent.tool
    def list_files(ctx: RunContext[str], path: str = ".") -> str:
        """List files and directories in the given workspace path."""
        full_path = os.path.join(ctx.deps, path)
        if not os.path.isdir(full_path):
            return f"Error: {path} is not a directory"
        entries = []
        for entry in sorted(Path(full_path).iterdir()):
            prefix = "d " if entry.is_dir() else "f "
            entries.append(prefix + entry.name)
        return "\n".join(entries) if entries else "(empty directory)"

    @agent.tool
    def run_command(ctx: RunContext[str], command: str) -> str:
        """Run a shell command in the workspace. Returns exit code, stdout, and stderr."""
        try:
            proc = subprocess.run(
                command,
                shell=True,
                cwd=ctx.deps,
                capture_output=True,
                text=True,
                timeout=300,
            )
            return (
                f"exit={proc.returncode}\n"
                f"--- stdout ---\n{proc.stdout}\n"
                f"--- stderr ---\n{proc.stderr}"
            )
        except subprocess.TimeoutExpired:
            return "Error: command timed out after 300 seconds"

    return agent


# -- Config helpers --

def load_config() -> dict:
    with open("config.json") as f:
        return json.load(f)


def build_system_prompt(config: dict) -> str:
    parts = [config.get("system_prompt", "")]
    for instr in config.get("instructions", []):
        name = instr.get("name", "")
        content = instr.get("content", "")
        if content:
            parts.append(f"\n\n---\n\n## {name}\n\n{content}")
    return "\n".join(parts)


def build_model_settings(config: dict) -> ModelSettings:
    model_config = config.get("model", {}).get("config") or {}
    kwargs: dict = {}
    if "temperature" in model_config:
        kwargs["temperature"] = float(model_config["temperature"])
    if "max_output_tokens" in model_config:
        kwargs["max_tokens"] = model_config["max_output_tokens"]
    return ModelSettings(**kwargs)


def setup_api_key(config: dict) -> None:
    """Set the provider-specific env var PydanticAI expects from config or environment."""
    model_cfg = config.get("model", {})
    api_key = model_cfg.get("api_key", "")
    if not api_key:
        api_key_env = model_cfg.get("api_key_env", "LLM_API_KEY")
        api_key = os.environ.get(api_key_env, "")
    if not api_key:
        print("Fatal: no API key in config.model.api_key or environment", file=sys.stderr)
        sys.exit(1)

    provider = model_cfg.get("provider", "openai")
    provider_env = PROVIDER_ENV_MAP.get(provider)
    if provider_env and provider_env not in os.environ:
        os.environ[provider_env] = api_key


def build_mcp_toolsets(config: dict) -> list[MCPServerStdio]:
    """Create MCPServerStdio instances from config.json mcp_servers."""
    servers = config.get("mcp_servers") or []
    toolsets = []
    for srv in servers:
        command = srv.get("command") or []
        if not command:
            continue
        env = None
        if srv.get("env"):
            env = {**os.environ, **{e["name"]: e["value"] for e in srv["env"]}}
        toolsets.append(MCPServerStdio(
            command[0],
            args=command[1:] + (srv.get("args") or []),
            env=env,
        ))
    return toolsets


def write_result(result: Result) -> None:
    with open("result.json", "w") as f:
        f.write(result.model_dump_json(indent=2, exclude_none=True))


# -- Main --

def main() -> None:
    try:
        config = load_config()
    except (FileNotFoundError, json.JSONDecodeError) as e:
        print(f"Fatal: {e}", file=sys.stderr)
        sys.exit(1)

    setup_api_key(config)

    model_cfg = config.get("model", {})
    provider = model_cfg.get("provider", "openai")
    model_name = model_cfg.get("model", "gpt-4o")
    model_string = f"{provider}:{model_name}"

    system_prompt = build_system_prompt(config)
    workspace = config.get("workspace", "/workspace")
    os.makedirs(workspace, exist_ok=True)

    timeout = config.get("limits", {}).get("timeout_seconds", 0)
    if timeout > 0:
        def timeout_handler(signum, frame):
            print(f"Agent timed out after {timeout}s", file=sys.stderr)
            write_result(Result())
            sys.exit(0)
        signal.signal(signal.SIGALRM, timeout_handler)
        signal.alarm(timeout)

    agent = create_agent(model_string, system_prompt)
    model_settings = build_model_settings(config)
    mcp_toolsets = build_mcp_toolsets(config)

    try:
        result = agent.run_sync(
            "Execute your instructions and produce the result.",
            deps=workspace,
            model_settings=model_settings,
            toolsets=mcp_toolsets,
        )
        output = result.output
        usage = result.usage()
        output.tokens_used = usage.input_tokens + usage.output_tokens
    except Exception as e:
        print(f"Agent error: {e}", file=sys.stderr)
        write_result(Result())
        sys.exit(1)

    write_result(output)


if __name__ == "__main__":
    main()
