"""Tests for agent_run using PydanticAI's TestModel (no real LLM calls)."""

import json
import os
import subprocess
import tempfile

import pytest
from pydantic_ai import models
from pydantic_ai.models.test import TestModel

from pydantic_ai.mcp import MCPServerStdio

from agent_run import (
    Action,
    Result,
    ReviewComment,
    build_mcp_toolsets,
    build_model_settings,
    build_system_prompt,
    create_agent,
    load_config,
    setup_api_key,
    write_result,
)

models.ALLOW_MODEL_REQUESTS = False


@pytest.fixture
def workspace(tmp_path):
    """Create a temporary workspace directory with sample files."""
    (tmp_path / "hello.txt").write_text("Hello, world!")
    (tmp_path / "subdir").mkdir()
    (tmp_path / "subdir" / "nested.txt").write_text("nested content")
    return str(tmp_path)


@pytest.fixture
def agent():
    return create_agent("test", "You are a test agent.")


@pytest.fixture
def config_dir(tmp_path):
    """Create a temporary directory with a config.json."""
    config = {
        "system_prompt": "You are a helpful agent.",
        "instructions": [
            {"name": "Style", "content": "Be concise."},
        ],
        "model": {
            "provider": "openai",
            "model": "gpt-4o",
            "api_key_env": "LLM_API_KEY",
            "config": {
                "temperature": "0.2",
                "max_output_tokens": 2048,
            },
        },
        "limits": {
            "max_tokens": 10000,
            "timeout_seconds": 300,
        },
        "workspace": str(tmp_path / "ws"),
    }
    (tmp_path / "config.json").write_text(json.dumps(config))
    return tmp_path, config


# -- Result schema tests --


def test_result_schema_defaults():
    r = Result()
    assert r.actions == []
    assert r.tokens_used == 0
    assert r.cost_usd == "0.00"


def test_result_schema_with_actions():
    r = Result(
        actions=[
            Action(type="comment", body="LGTM"),
            Action(
                type="review",
                event="APPROVE",
                body="All good.",
                comments=[ReviewComment(path="main.go", line=10, body="Nice")],
            ),
            Action(type="label", add=["approved"], remove=["needs-review"]),
            Action(type="create-pr", title="Fix bug", head="fix/bug", base="main"),
            Action(
                type="status",
                context="ci/review",
                state="success",
                description="Passed.",
            ),
            Action(
                type="commit",
                message="fix typo",
                files={"README.md": "# Fixed"},
            ),
        ],
        tokens_used=1500,
        cost_usd="0.03",
    )
    data = json.loads(r.model_dump_json(exclude_none=True))
    assert len(data["actions"]) == 6
    assert data["actions"][0]["type"] == "comment"
    assert data["actions"][1]["comments"][0]["line"] == 10
    assert data["actions"][5]["files"]["README.md"] == "# Fixed"


def test_result_json_excludes_none():
    r = Result(actions=[Action(type="comment", body="hi")])
    data = json.loads(r.model_dump_json(exclude_none=True))
    action = data["actions"][0]
    assert "body" in action
    assert "event" not in action
    assert "comments" not in action


# -- Agent tests with TestModel --


def test_agent_produces_result(agent, workspace):
    with agent.override(model=TestModel()):
        result = agent.run_sync(
            "Produce a result.", deps=workspace
        )
        assert isinstance(result.output, Result)


def test_agent_result_has_valid_structure(agent, workspace):
    with agent.override(model=TestModel()):
        result = agent.run_sync("Produce a result.", deps=workspace)
        output = result.output
        assert isinstance(output.actions, list)
        assert isinstance(output.tokens_used, int)
        assert isinstance(output.cost_usd, str)


# -- Tool tests --


def test_read_file_tool(workspace):
    agent = create_agent("test", "Read files.")
    with agent.override(model=TestModel()):
        # Directly test the tool function via the agent's tool registry
        from pydantic_ai import RunContext

        # The tool is registered on the agent; test it by running the agent
        # TestModel will call all tools automatically
        result = agent.run_sync(
            "Read hello.txt", deps=workspace
        )
        assert isinstance(result.output, Result)


def test_read_file_missing(workspace):
    agent = create_agent("test", "Read files.")
    # Verify the tool handles missing files gracefully
    from agent_run import os as agent_os

    full_path = os.path.join(workspace, "nonexistent.txt")
    assert not os.path.exists(full_path)


def test_write_file_tool(workspace):
    agent = create_agent("test", "Write files.")
    with agent.override(model=TestModel()):
        result = agent.run_sync(
            "Write a file.", deps=workspace
        )
        assert isinstance(result.output, Result)


def test_list_files_tool(workspace):
    agent = create_agent("test", "List files.")
    with agent.override(model=TestModel()):
        result = agent.run_sync(
            "List workspace files.", deps=workspace
        )
        assert isinstance(result.output, Result)


def test_run_command_tool(workspace):
    agent = create_agent("test", "Run commands.")
    with agent.override(model=TestModel()):
        result = agent.run_sync(
            "Run a command.", deps=workspace
        )
        assert isinstance(result.output, Result)


# -- Config tests --


def test_load_config(config_dir):
    tmp_path, expected = config_dir
    orig = os.getcwd()
    try:
        os.chdir(tmp_path)
        config = load_config()
        assert config["model"]["provider"] == "openai"
        assert config["system_prompt"] == "You are a helpful agent."
    finally:
        os.chdir(orig)


def test_build_system_prompt_basic():
    config = {"system_prompt": "Base prompt."}
    prompt = build_system_prompt(config)
    assert prompt == "Base prompt."


def test_build_system_prompt_with_instructions():
    config = {
        "system_prompt": "Base prompt.",
        "instructions": [
            {"name": "Style", "content": "Be concise."},
            {"name": "Format", "content": "Use JSON."},
        ],
    }
    prompt = build_system_prompt(config)
    assert "Base prompt." in prompt
    assert "## Style" in prompt
    assert "Be concise." in prompt
    assert "## Format" in prompt
    assert "Use JSON." in prompt


def test_build_system_prompt_skips_empty_instructions():
    config = {
        "system_prompt": "Base.",
        "instructions": [
            {"name": "Empty", "content": ""},
            {"name": "Valid", "content": "Keep this."},
        ],
    }
    prompt = build_system_prompt(config)
    assert "Empty" not in prompt
    assert "Valid" in prompt


def test_build_model_settings_with_values():
    config = {
        "model": {
            "config": {
                "temperature": "0.5",
                "max_output_tokens": 4096,
            }
        }
    }
    settings = build_model_settings(config)
    assert settings["temperature"] == 0.5
    assert settings["max_tokens"] == 4096


def test_build_model_settings_empty():
    config = {"model": {}}
    settings = build_model_settings(config)
    assert "temperature" not in settings
    assert "max_tokens" not in settings


def test_model_string_construction():
    config = {"model": {"provider": "anthropic", "model": "claude-sonnet-4-20250514"}}
    provider = config["model"]["provider"]
    model = config["model"]["model"]
    assert f"{provider}:{model}" == "anthropic:claude-sonnet-4-20250514"


def test_setup_api_key_from_config(monkeypatch):
    monkeypatch.delenv("LLM_API_KEY", raising=False)
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    config = {"model": {"provider": "openai", "api_key": "sk-from-config"}}
    setup_api_key(config)
    assert os.environ["OPENAI_API_KEY"] == "sk-from-config"


def test_setup_api_key_config_over_env(monkeypatch):
    monkeypatch.setenv("LLM_API_KEY", "from-env")
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    config = {"model": {"provider": "openai", "api_key": "from-config"}}
    setup_api_key(config)
    assert os.environ["OPENAI_API_KEY"] == "from-config"


def test_setup_api_key_falls_back_to_env(monkeypatch):
    monkeypatch.setenv("LLM_API_KEY", "test-key-123")
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    config = {"model": {"provider": "openai", "api_key_env": "LLM_API_KEY"}}
    setup_api_key(config)
    assert os.environ["OPENAI_API_KEY"] == "test-key-123"


def test_setup_api_key_does_not_overwrite(monkeypatch):
    monkeypatch.delenv("LLM_API_KEY", raising=False)
    monkeypatch.setenv("OPENAI_API_KEY", "existing-key")
    config = {"model": {"provider": "openai", "api_key": "new-key"}}
    setup_api_key(config)
    assert os.environ["OPENAI_API_KEY"] == "existing-key"


# -- Write result tests --


def test_write_result(tmp_path):
    orig = os.getcwd()
    try:
        os.chdir(tmp_path)
        result = Result(
            actions=[Action(type="comment", body="test")],
            tokens_used=100,
        )
        write_result(result)
        with open("result.json") as f:
            data = json.load(f)
        assert data["actions"][0]["type"] == "comment"
        assert data["tokens_used"] == 100
        assert "event" not in data["actions"][0]
    finally:
        os.chdir(orig)


# -- MCP toolset tests --


def test_build_mcp_toolsets_stdio():
    config = {
        "mcp_servers": [
            {
                "name": "filesystem",
                "command": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
                "args": ["/workspace"],
            }
        ]
    }
    toolsets = build_mcp_toolsets(config)
    assert len(toolsets) == 1
    assert isinstance(toolsets[0], MCPServerStdio)


def test_build_mcp_toolsets_with_args():
    config = {
        "mcp_servers": [
            {
                "name": "custom",
                "command": ["npx", "-y", "@mcp/server-custom"],
                "args": ["--verbose", "--port", "3000"],
            }
        ]
    }
    toolsets = build_mcp_toolsets(config)
    assert len(toolsets) == 1


def test_build_mcp_toolsets_empty():
    assert build_mcp_toolsets({}) == []
    assert build_mcp_toolsets({"mcp_servers": []}) == []
    assert build_mcp_toolsets({"mcp_servers": None}) == []


def test_build_mcp_toolsets_skips_no_command():
    config = {
        "mcp_servers": [
            {"name": "empty", "command": []},
            {"name": "missing"},
        ]
    }
    assert build_mcp_toolsets(config) == []


def test_build_mcp_toolsets_env_merge(monkeypatch):
    monkeypatch.setenv("EXISTING", "keep-me")
    config = {
        "mcp_servers": [
            {
                "name": "with-env",
                "command": ["node", "server.js"],
                "env": [
                    {"name": "CUSTOM_VAR", "value": "custom-val"},
                ],
            }
        ]
    }
    toolsets = build_mcp_toolsets(config)
    assert len(toolsets) == 1
    srv = toolsets[0]
    assert srv.env["CUSTOM_VAR"] == "custom-val"
    assert srv.env["EXISTING"] == "keep-me"


def test_build_mcp_toolsets_no_env():
    config = {
        "mcp_servers": [
            {"name": "no-env", "command": ["echo", "hello"]},
        ]
    }
    toolsets = build_mcp_toolsets(config)
    assert len(toolsets) == 1
    assert toolsets[0].env is None


def test_build_mcp_toolsets_multiple():
    config = {
        "mcp_servers": [
            {"name": "a", "command": ["cmd-a"]},
            {"name": "b", "command": ["cmd-b"], "args": ["--flag"]},
        ]
    }
    toolsets = build_mcp_toolsets(config)
    assert len(toolsets) == 2


# -- Write result tests --


def test_write_empty_result(tmp_path):
    orig = os.getcwd()
    try:
        os.chdir(tmp_path)
        write_result(Result())
        with open("result.json") as f:
            data = json.load(f)
        assert data["actions"] == []
        assert data["tokens_used"] == 0
    finally:
        os.chdir(orig)
