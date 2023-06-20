#include <iostream>
#include <Python.h>
#include <frameobject.h>
#include <stdio.h>
#include <string>
#include <sys/stat.h>

using std::cerr;
using std::cout;
using std::endl;
using std::string;
using std::wcout;

#if defined(__x86_64__) || defined(_M_X64)
#define ARCH "_x64"
#elif defined(__aarch64__) || defined(_M_ARM64)
#define ARCH "_arm64"
#else
#define ARCH "_undefined"
#endif

#if defined(__linux__)
// all includes are defined below
#elif _WIN32
#include <direct.h>
#else
// MacOS
#include <mach-o/dyld.h>
#include <limits.h>
#include <fcntl.h>
#endif

#ifndef _WIN32
#include <filesystem>
#include <fcntl.h>
#endif

wchar_t *GetWC(const char *c) {
  const size_t cSize = strlen(c)+1;
  wchar_t* wc = new wchar_t[cSize];
  mbstowcs (wc, c, cSize);

  return wc;
}

bool errorOccurred() {
  if (PyErr_Occurred()) {
    /* if we want to get the exception string */
    PyObject *errtype, *errvalue, *errtraceback;
    PyErr_Fetch(&errtype, &errvalue, &errtraceback);
    PyErr_NormalizeException(&errtype, &errvalue, &errtraceback);
    if(errvalue != nullptr) {
      PyObject *s = PyObject_Str(errvalue);
      Py_ssize_t size;
      const char* exception = PyUnicode_AsUTF8AndSize(s, &size);
      printf("PAKIKI_PYTHON_INTERPRETER_ERROR\n");
      printf("%s\n", exception);
      Py_DECREF(s);
    }

    PyTracebackObject* traceback = (PyTracebackObject*)errtraceback;
    if(traceback) {
      do {
        PyFrameObject *frame = traceback->tb_frame;
        int lineNumber = PyFrame_GetLineNumber(frame);
        PyObject *code_obj = (PyObject *)PyFrame_GetCode(frame);
        PyObject *filename_obj = PyObject_GetAttrString(code_obj, "co_filename");
        const char *filename = PyUnicode_AsUTF8(filename_obj);

        printf("%s:%d\n", filename, lineNumber);
        traceback = traceback->tb_next;
        Py_XDECREF(filename_obj);

      } while(traceback != nullptr);
    }

    Py_XDECREF(errvalue);
    Py_XDECREF(errtype);
    Py_XDECREF(traceback);

    return true;
  }
  else {
    return false;
  }
}

char* concatenateDir(const char* path) {
  char* currDir = (char*)malloc(102400);
  currDir = getcwd(currDir, 102400);
  if(currDir == nullptr) {
    fprintf(stderr, "%d", errno);
  }

  size_t currDirLen = strlen(currDir);

  if(currDirLen > (102400 - strlen(path) - 3))
    return nullptr;

  memcpy(currDir + currDirLen, path, strlen(path) + 1);

  return currDir;
}

char* getDir() {
#ifdef _WIN32
  char* dir = concatenateDir("\\python39");
#elif defined(__linux__)
  const char* pythonSubdir = "/python311";

  // identify the current path of the executable - so that we can run cleanly under flatpak
  std::filesystem::path path = std::filesystem::canonical("/proc/self/exe").parent_path();
  const char* pathStr = path.c_str();

  char* dir = (char*)malloc(strlen(pathStr) + strlen(pythonSubdir) + 1);
  memcpy(dir, pathStr, strlen(pathStr));
  memcpy(dir + strlen(pathStr), pythonSubdir, strlen(pythonSubdir) + 1);

  if(!std::filesystem::exists(dir)) {
    return NULL;
  }

#else
  char* dir = concatenateDir("/python310" ARCH);
  int fd = open(dir, O_RDONLY);
  if(fd == -1) {
    free(dir);

    char exePath [PATH_MAX];
    uint32_t bufsize = PATH_MAX;
    if(_NSGetExecutablePath(exePath, &bufsize)) {
      return nullptr;
    }

    const char* pythonSubdir = ("/python310" ARCH);

    // identify the current path of the executable - so that we can run cleanly in app bundles
    std::filesystem::path path = std::filesystem::canonical(exePath).parent_path();
    const char* pathStr = path.c_str();

    dir = (char*)malloc(strlen(pathStr) + strlen(pythonSubdir) + 1);
    memcpy(dir, pathStr, strlen(pathStr));
    memcpy(dir + strlen(pathStr), pythonSubdir, strlen(pythonSubdir) + 1);

    fd = open(dir, O_RDONLY);

    if(fd == -1)
      return nullptr;
  }
  
  close(fd);
#endif
  
  struct stat sb;
  int statResult = stat(dir, &sb);
  if(statResult != 0 || !S_ISDIR(sb.st_mode)) {
    return nullptr;
  }

  return dir;
}

bool runDiscreteCode(string code, const char* filename, PyObject* py_dict) {
  PyObject* compiledObject = Py_CompileString(code.c_str(), filename, Py_file_input);

  if(errorOccurred()) {
    return false;
  }

  PyEval_EvalCode(compiledObject, py_dict, py_dict);
  if(errorOccurred()) {
    return false;
  }

  return true;
}

bool runPythonScript() {
  PyThreadState* globalThreadState = PyThreadState_Get();
  PyThreadState* threadState = Py_NewInterpreter();
  if(threadState == nullptr) {
    return false;
  }

  PyThreadState_Swap(threadState);

  string pythonCode;
  string line;
  std::getline(std::cin, line, '\n');

  PyObject *py_main = PyImport_AddModule("__main__");
  PyObject *py_dict = PyModule_GetDict(py_main);

  bool errorThrown = false;

  string filename;
  bool endInterpreter = false;

  while(line != "PAKIKI_PYTHON_INTERPRETER_END_OF_SCRIPT" && line != "PAKIKI_PYTHON_INTERPRETER_END_INTERPRETER") {
    if(filename == "") {
      filename = line;
    }
    else if(line == "PAKIKI_PYTHON_INTERPRETER_END_OF_BLOCK") {
      if(pythonCode != "") {
        if(runDiscreteCode(pythonCode, filename.c_str(), py_dict) == false) {
          errorThrown = true;
          break;
        }
      }
      pythonCode = "";
      filename = "";
      cerr << "PAKIKI_PYTHON_INTERPRETER_READY" << endl;
    }
    else {
      pythonCode += line + "\n";
    }
    std::getline(std::cin, line, '\n');
  }

  if(line == "PAKIKI_PYTHON_INTERPRETER_END_INTERPRETER") {
    endInterpreter = true;
  }

  if(!errorThrown) {
    runDiscreteCode(pythonCode, filename.c_str(), py_dict);
  }

  cerr << "PAKIKI_PYTHON_INTERPRETER_SCRIPT_FINISHED" << endl;

  Py_EndInterpreter(PyThreadState_Get());
  PyThreadState_Swap(globalThreadState);

  return endInterpreter;
}

int main(int argc, char *argv[]) {
  PyStatus status;

  PyConfig config;
  PyConfig_InitPythonConfig(&config);
  config.isolated = 1;

  /* Decode command line arguments.
    Implicitly preinitialize Python (in isolated mode). */
  status = PyConfig_SetBytesArgv(&config, argc, argv);
  if (PyStatus_Exception(status)) {
    goto exception;
  }

  status = Py_InitializeFromConfig(&config);
  if (PyStatus_Exception(status)) {
    goto exception;
  }
  PyConfig_Clear(&config);

  runPythonScript();

  Py_Finalize();

  return 0;

exception:
  PyConfig_Clear(&config);
  if (PyStatus_IsExit(status)) {
    return status.exitcode;
  }
  /* Display the error message and exit the process with
    non-zero exit code */
  Py_ExitStatusException(status);

  
}