package deps

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoImports(t *testing.T) {
	content := []byte(`package app

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	testify "github.com/stretchr/testify/require"
)
`)
	got := Imports("main.go", content)
	require.ElementsMatch(t, []string{"github.com/spf13/cobra", "github.com/stretchr/testify/require"}, names(got))
	for _, p := range got {
		require.Equal(t, GoMod, p.Ecosystem)
		require.Positive(t, p.Line)
	}
}

func TestGoImportsUnparsable(t *testing.T) {
	require.Empty(t, Imports("broken.go", []byte("not go at all {")))
}

func TestPythonImports(t *testing.T) {
	content := []byte(`import os
import requests
import numpy as np, pandas
from flask import Flask
from . import sibling
from .relative import thing
from collections.abc import Mapping
import yaml
from PIL import Image
`)
	got := Imports("app.py", content)
	require.ElementsMatch(t,
		[]string{"requests", "numpy", "pandas", "flask", "pyyaml", "pillow"},
		names(got))
}

func TestJSImports(t *testing.T) {
	content := []byte(`import express from 'express';
import { useState } from "react";
import '@scope/pkg/styles.css';
import fs from 'node:fs';
import path from 'path';
import helper from './helper';
import alias from '@/components/button';
export { thing } from 'lodash/fp';
const chalk = require('chalk');
const lazy = await import('dayjs');
`)
	got := Imports("app.ts", content)
	require.ElementsMatch(t,
		[]string{"express", "react", "@scope/pkg", "lodash", "chalk", "dayjs"},
		names(got))
}

func TestImportsUnknownLanguage(t *testing.T) {
	require.Empty(t, Imports("main.rs", []byte("use serde;")))
}
