// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {BaseAccount} from "@account-abstraction/contracts/core/BaseAccount.sol";
import {IEntryPoint} from "@account-abstraction/contracts/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "@account-abstraction/contracts/interfaces/PackedUserOperation.sol";
import {SIG_VALIDATION_FAILED, SIG_VALIDATION_SUCCESS} from "@account-abstraction/contracts/core/Helpers.sol";
import {ECDSA} from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import {MessageHashUtils} from "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";
import {Initializable} from "@openzeppelin/contracts/proxy/utils/Initializable.sol";

/// @title SmartAccount
/// @notice ERC-4337 compliant smart account with ECDSA owner validation.
///         Designed to be deployed as a proxy via SmartAccountFactory (CREATE2).
///         Session key support will be added in a subsequent issue.
contract SmartAccount is BaseAccount, Initializable {
    using ECDSA for bytes32;
    using MessageHashUtils for bytes32;

    // ───────────────────────────── Storage ─────────────────────────────

    /// @notice The EOA that owns this account and can sign UserOperations.
    address public owner;

    /// @notice The canonical EntryPoint v0.7 contract. Set once at construction
    ///         and shared across all proxies (lives in the implementation, not storage).
    IEntryPoint private immutable _ENTRY_POINT;

    // ───────────────────────────── Events ──────────────────────────────

    event SmartAccountInitialized(IEntryPoint indexed entryPoint, address indexed owner);

    // ───────────────────────────── Errors ──────────────────────────────

    error OnlyOwnerOrEntryPoint();
    error OnlyOwner();
    error CallFailed(bytes returnData);

    // ───────────────────────────── Modifiers ───────────────────────────

    /// @notice Restricts to the EntryPoint (during UserOp execution) or the owner (direct calls).
    modifier onlyOwnerOrEntryPoint() {
        _checkOwnerOrEntryPoint();
        _;
    }

    /// @notice Restricts to the owner only.
    modifier onlyOwner() {
        _checkOwner();
        _;
    }

    // ─────────────────────────── Constructor ───────────────────────────

    /// @notice Sets the EntryPoint. Called once on the *implementation* contract.
    ///         `_disableInitializers()` prevents anyone from calling `initialize`
    ///         on the implementation itself — only proxies can be initialized.
    /// @param entryPoint_ The EntryPoint v0.7 address (same on all EVM chains).
    constructor(IEntryPoint entryPoint_) {
        _ENTRY_POINT = entryPoint_;
        _disableInitializers();
    }

    // ─────────────────────────── Initializer ───────────────────────────

    /// @notice Initializes a proxy clone. Called once by the factory during CREATE2 deployment.
    /// @param owner_ The EOA that will own this smart account.
    function initialize(address owner_) external initializer {
        require(owner_ != address(0), "invalid owner");
        owner = owner_;
        emit SmartAccountInitialized(_ENTRY_POINT, owner_);
    }

    // ──────────────────── BaseAccount implementation ───────────────────

    /// @inheritdoc BaseAccount
    function entryPoint() public view override returns (IEntryPoint) {
        return _ENTRY_POINT;
    }

    /// @notice Validates the UserOperation signature.
    ///         Recovers the signer from the signature and checks it matches the owner.
    ///
    ///         The flow (called by BaseAccount.validateUserOp):
    ///         1. `userOpHash` is the hash of the UserOp (already includes entryPoint + chainId).
    ///         2. We convert it to an Ethereum Signed Message hash (prepend "\x19Ethereum Signed Message:\n32").
    ///         3. ECDSA.recover extracts the signer address from the signature.
    ///         4. If signer == owner → return 0 (success). Otherwise → return 1 (failure, no revert).
    ///
    /// @inheritdoc BaseAccount
    function _validateSignature(
        PackedUserOperation calldata userOp,
        bytes32 userOpHash
    ) internal view override returns (uint256 validationData) {
        // Convert the raw hash to an eth_sign compatible hash.
        // This is what wallets (MetaMask) actually sign — they prepend the EIP-191 prefix.
        bytes32 ethSignedHash = userOpHash.toEthSignedMessageHash();

        // Recover the signer. If the signature is invalid, this returns address(0).
        address recovered = ethSignedHash.recover(userOp.signature);

        // SIG_VALIDATION_FAILED (1) tells the EntryPoint the sig is bad — without reverting.
        // Reverting would be a hard failure (bad nonce, malformed data), not a sig mismatch.
        if (recovered != owner) {
            return SIG_VALIDATION_FAILED;
        }
        return SIG_VALIDATION_SUCCESS;
    }

    // ──────────────────────────── Execution ────────────────────────────

    /// @notice Execute a single call from this account.
    ///         Called by the EntryPoint after successful validation (via userOp.callData),
    ///         or directly by the owner for convenience.
    /// @param target  The contract (or EOA) to call.
    /// @param value   The ETH value to send.
    /// @param data    The calldata (function selector + arguments).
    function execute(address target, uint256 value, bytes calldata data) external onlyOwnerOrEntryPoint {
        (bool success, bytes memory returnData) = target.call{value: value}(data);
        if (!success) {
            revert CallFailed(returnData);
        }
    }

    /// @notice Execute multiple calls in a single UserOperation.
    ///         Useful for batching (e.g., approve + swap in one op).
    /// @param targets  Array of addresses to call.
    /// @param values   Array of ETH values to send.
    /// @param calldatas Array of calldata payloads.
    function executeBatch(
        address[] calldata targets,
        uint256[] calldata values,
        bytes[] calldata calldatas
    ) external onlyOwnerOrEntryPoint {
        require(targets.length == values.length && values.length == calldatas.length, "length mismatch");
        for (uint256 i = 0; i < targets.length; i++) {
            (bool success, bytes memory returnData) = targets[i].call{value: values[i]}(calldatas[i]);
            if (!success) {
                revert CallFailed(returnData);
            }
        }
    }

    // ───────────────────────────── Receive ─────────────────────────────

    /// @notice Accept ETH deposits. The account needs ETH to pay for gas
    ///         (deposited to the EntryPoint, or held directly).
    receive() external payable {}

    // ──────────────────────── Internal helpers ─────────────────────────

    function _checkOwnerOrEntryPoint() internal view {
        if (msg.sender != address(entryPoint()) && msg.sender != owner) {
            revert OnlyOwnerOrEntryPoint();
        }
    }

    function _checkOwner() internal view {
        if (msg.sender != owner) {
            revert OnlyOwner();
        }
    }
}
